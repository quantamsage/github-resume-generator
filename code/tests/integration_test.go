// Package tests runs the full application stack against fake GitHub and
// Claude API servers built from the fixtures in testdata/.
package tests

import (
	"bytes"
	"encoding/json"
	"html"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/judie-paul/resume-generator/internal/ai"
	"github.com/judie-paul/resume-generator/internal/github"
	"github.com/judie-paul/resume-generator/internal/handler"
	"github.com/judie-paul/resume-generator/internal/middleware"
	"github.com/judie-paul/resume-generator/internal/resume"
	"github.com/judie-paul/resume-generator/internal/web"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// fakeGitHub serves the fixtures for user "devjane" and 404 for anyone else.
func fakeGitHub(t *testing.T) *httptest.Server {
	user, repos, readme := fixture(t, "user.json"), fixture(t, "repos.json"), fixture(t, "taskqueue-readme.md")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/users/devjane":
			w.Write(user)
		case "/users/devjane/repos":
			w.Write(repos)
		case "/repos/devjane/taskqueue/readme":
			w.Write(readme)
		case "/repos/devjane/taskqueue/languages":
			w.Write([]byte(`{"Go":52000,"Shell":800,"Dockerfile":300}`))
		case "/repos/devjane/no-readme/languages":
			w.Write([]byte(`{"Python":3000}`))
		case "/users/ratelimited":
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.WriteHeader(http.StatusForbidden)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

type fakeClaude struct {
	*httptest.Server
	mu      sync.Mutex
	prompts []string
	reply   []byte
}

func newFakeClaude(t *testing.T, reply []byte) *fakeClaude {
	f := &fakeClaude{reply: reply}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "test-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var req struct {
			Messages []struct {
				Content []struct{ Text string } `json:"content"`
			} `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		f.mu.Lock()
		f.prompts = append(f.prompts, req.Messages[0].Content[0].Text)
		f.mu.Unlock()
		resp, _ := json.Marshal(map[string]any{
			"content":     []map[string]string{{"type": "text", "text": string(f.reply)}},
			"stop_reason": "end_turn",
			"usage":       map[string]int{"input_tokens": 1000, "output_tokens": 400},
		})
		w.Header().Set("Content-Type", "application/json")
		w.Write(resp)
	}))
	t.Cleanup(f.Close)
	return f
}

func newApp(t *testing.T, ghURL, claudeURL string) *httptest.Server {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	gh, err := github.NewClient(ghURL, "", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	var gen resume.JSONGenerator
	if claudeURL != "" {
		c, err := ai.NewClient(claudeURL, "test-key", "claude-opus-5-5", "medium", 5*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		gen = c
	}
	svc := resume.NewService(gh, gen, resume.Options{MaxRepos: 30, MaxSelected: 8, MaxReadmeChars: 6000}, log)
	pages, err := web.Pages()
	if err != nil {
		t.Fatal(err)
	}
	h := handler.New(svc, pages, web.Static(), log, handler.Config{
		MaxConcurrentGenerations: 2, GenerateTimeout: 10 * time.Second, AnalyzeTimeout: 10 * time.Second, MaxSelected: 8,
	})
	limiter := middleware.NewRateLimiter(1000, false)
	app := httptest.NewServer(middleware.Chain(h.Routes(limiter.Middleware),
		middleware.Recover(log), middleware.Logger(log), middleware.SecurityHeaders, middleware.SameOrigin))
	t.Cleanup(app.Close)
	return app
}

var dataResumeRE = regexp.MustCompile(`data-resume="([^"]*)"`)

func TestEndToEnd(t *testing.T) {
	gh := fakeGitHub(t)
	claude := newFakeClaude(t, fixture(t, "claude-response.json"))
	app := newApp(t, gh.URL, claude.URL)

	// 1. Analyze: repositories listed, fork listed but not preselected.
	resp, err := http.Get(app.URL + "/analyze?username=" + url.QueryEscape("https://github.com/devjane"))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	page := string(body)
	if resp.StatusCode != 200 {
		t.Fatalf("analyze status %d", resp.StatusCode)
	}
	if !regexp.MustCompile(`value="taskqueue"\s+checked`).MatchString(page) {
		t.Error("taskqueue should be preselected")
	}
	if regexp.MustCompile(`value="forked-lib"\s+checked`).MatchString(page) || !strings.Contains(page, `value="forked-lib"`) {
		t.Error("fork should be listed but not preselected")
	}

	// 2. Generate.
	resp, err = http.PostForm(app.URL+"/generate", url.Values{
		"username": {"devjane"}, "repos": {"taskqueue", "no-readme"},
		"email": {"jane@example.com"}, "job_description": {"Senior Go engineer, Kubernetes required"},
	})
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("generate status %d: %s", resp.StatusCode, body)
	}
	m := dataResumeRE.FindSubmatch(body)
	if m == nil {
		t.Fatal("editor page has no resume data")
	}
	var r resume.Resume
	if err := json.Unmarshal([]byte(html.UnescapeString(string(m[1]))), &r); err != nil {
		t.Fatal(err)
	}
	if r.Name != "Jane Dev" || r.Contact.Email != "jane@example.com" || r.Contact.Website != "https://jane.dev" {
		t.Errorf("profile/contact merge wrong: %+v", r.Contact)
	}
	if len(r.Projects) != 1 || r.Projects[0].URL != "https://github.com/devjane/taskqueue" {
		t.Errorf("invented project should be dropped and URL come from GitHub: %+v", r.Projects)
	}
	if len(r.Experience)+len(r.Education)+len(r.Certifications) != 0 {
		t.Error("AI must not populate experience, education or certifications")
	}

	// The prompt carried sanitized evidence inside the untrusted data block.
	if len(claude.prompts) != 1 {
		t.Fatalf("expected one Claude call, got %d", len(claude.prompts))
	}
	prompt := claude.prompts[0]
	if !strings.Contains(prompt, "SKIP LOCKED") || !strings.Contains(prompt, "Kubernetes required") {
		t.Error("prompt missing README evidence or job description")
	}
	if strings.Contains(prompt, "ignore all previous instructions") {
		t.Error("README HTML comment (hidden injection) should be stripped before sending")
	}
	if strings.Contains(prompt, "forked-lib") {
		t.Error("unselected repository sent to the model")
	}

	// 3. Export PDF with the edited resume.
	r.Experience = []resume.Experience{{Title: "Backend Developer", Company: "Acme", Start: "2023", Highlights: []string{"Built APIs"}}}
	reqBody, _ := json.Marshal(map[string]any{"resume": r, "template": "ats", "paper": "a4"})
	resp, err = http.Post(app.URL+"/api/pdf", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatal(err)
	}
	pdfBytes, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !bytes.HasPrefix(pdfBytes, []byte("%PDF-1.4")) {
		t.Fatalf("pdf export failed: %d", resp.StatusCode)
	}
	if !bytes.Contains(pdfBytes, []byte("/URI (https://github.com/devjane/taskqueue)")) {
		t.Error("PDF should contain a clickable project link")
	}
}

func TestWithoutAIProducesDraft(t *testing.T) {
	app := newApp(t, fakeGitHub(t).URL, "")
	resp, err := http.PostForm(app.URL+"/generate", url.Values{"username": {"devjane"}, "repos": {"taskqueue"}})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !dataResumeRE.Match(body) {
		t.Fatalf("draft generation failed: %d", resp.StatusCode)
	}
}

func TestUpstreamFailures(t *testing.T) {
	gh := fakeGitHub(t)

	app := newApp(t, gh.URL, newFakeClaude(t, []byte(`{"headline": "only this"}`)).URL)
	resp, _ := http.PostForm(app.URL+"/generate", url.Values{"username": {"devjane"}, "repos": {"taskqueue"}})
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("malformed AI output: got %d, want 502", resp.StatusCode)
	}

	for path, want := range map[string]int{
		"/analyze?username=ghost":       404,
		"/analyze?username=ratelimited": 429,
	} {
		resp, err := http.Get(app.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Errorf("%s: got %d want %d", path, resp.StatusCode, want)
		}
	}

	// GitHub unreachable.
	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := dead.URL
	dead.Close()
	app = newApp(t, deadURL, "")
	resp, err := http.Get(app.URL + "/analyze?username=devjane")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("network failure: got %d, want 502", resp.StatusCode)
	}
}

func TestCrossSitePostRejected(t *testing.T) {
	app := newApp(t, fakeGitHub(t).URL, "")
	req, _ := http.NewRequest("POST", app.URL+"/api/pdf", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://attacker.example")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("got %d", resp.StatusCode)
	}
}
