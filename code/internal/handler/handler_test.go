package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/judie-paul/resume-generator/internal/github"
	"github.com/judie-paul/resume-generator/internal/resume"
	"github.com/judie-paul/resume-generator/internal/web"
)

type fakeSvc struct {
	analyzeErr error
	genErr     error
	block      chan struct{}
	gotRepos   []string
	gotInput   resume.UserInput
}

func (f *fakeSvc) AIEnabled() bool { return true }

func (f *fakeSvc) Analyze(ctx context.Context, u string) (*resume.Analysis, error) {
	if f.analyzeErr != nil {
		return nil, f.analyzeErr
	}
	return &resume.Analysis{
		User:        &github.User{Login: u, Name: "<b>Evil</b>", HTMLURL: "https://github.com/" + u},
		Repos:       []resume.ScoredRepo{{Repo: github.Repo{Name: "repo1", Description: "<script>alert(1)</script>", PushedAt: time.Now()}, Selected: true}},
		MaxSelected: 8,
	}, nil
}

func (f *fakeSvc) Generate(ctx context.Context, u string, repos []string, in resume.UserInput) (*resume.Resume, error) {
	if f.block != nil {
		<-f.block
	}
	f.gotRepos, f.gotInput = repos, in
	if f.genErr != nil {
		return nil, f.genErr
	}
	r := resume.Sample()
	r.Name = `</script><script>alert(1)</script>`
	return r, nil
}

func newServer(t *testing.T, svc *fakeSvc) *httptest.Server {
	t.Helper()
	pages, err := web.Pages()
	if err != nil {
		t.Fatal(err)
	}
	h := New(svc, pages, web.Static(), slog.New(slog.NewTextHandler(io.Discard, nil)), Config{
		MaxConcurrentGenerations: 1, GenerateTimeout: 5 * time.Second, AnalyzeTimeout: 5 * time.Second, MaxSelected: 2,
	})
	srv := httptest.NewServer(h.Routes(func(h http.Handler) http.Handler { return h }))
	t.Cleanup(srv.Close)
	return srv
}

func get(t *testing.T, u string) (int, string) {
	t.Helper()
	resp, err := http.Get(u)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestPages(t *testing.T) {
	srv := newServer(t, &fakeSvc{})
	for path, want := range map[string]int{"/": 200, "/editor": 200, "/healthz": 200, "/static/app.css": 200, "/nope": 404} {
		if code, _ := get(t, srv.URL+path); code != want {
			t.Errorf("%s: got %d want %d", path, code, want)
		}
	}
}

func TestAnalyze(t *testing.T) {
	srv := newServer(t, &fakeSvc{})
	code, body := get(t, srv.URL+"/analyze?username="+url.QueryEscape("https://github.com/octocat"))
	if code != 200 || !strings.Contains(body, `value="repo1"`) {
		t.Fatalf("got %d", code)
	}
	if strings.Contains(body, "<script>alert") || strings.Contains(body, "<b>Evil</b>") {
		t.Fatal("untrusted GitHub data was not escaped")
	}

	code, body = get(t, srv.URL+"/analyze?username=bad%20name")
	if code != 400 || !strings.Contains(body, "valid GitHub username") {
		t.Fatalf("invalid username: %d", code)
	}

	srv = newServer(t, &fakeSvc{analyzeErr: github.ErrNotFound})
	if code, body = get(t, srv.URL+"/analyze?username=ghost"); code != 404 || !strings.Contains(body, "could not be found") {
		t.Fatalf("not found: %d", code)
	}

	srv = newServer(t, &fakeSvc{analyzeErr: &github.RateLimitError{}})
	if code, _ = get(t, srv.URL+"/analyze?username=x"); code != 429 {
		t.Fatalf("rate limit: %d", code)
	}
}

func TestGenerate(t *testing.T) {
	svc := &fakeSvc{}
	srv := newServer(t, svc)
	form := url.Values{"username": {"octocat"}, "repos": {"a", "b", "c"}, "email": {"me@example.com"}, "job_description": {"Go dev"}}
	resp, err := http.PostForm(srv.URL+"/generate", form)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(b), `id="editor-app"`) {
		t.Fatalf("got %d", resp.StatusCode)
	}
	if strings.Contains(string(b), "<script>alert") {
		t.Fatal("resume JSON not escaped in attribute")
	}
	if len(svc.gotRepos) != 2 || svc.gotInput.JobDescription != "Go dev" {
		t.Fatalf("selection not capped or input lost: %v %+v", svc.gotRepos, svc.gotInput)
	}

	for name, f := range map[string]url.Values{
		"no repos":  {"username": {"octocat"}},
		"bad user":  {"username": {"-x-"}, "repos": {"a"}},
		"bad email": {"username": {"octocat"}, "repos": {"a"}, "email": {"nope"}},
	} {
		resp, _ := http.PostForm(srv.URL+"/generate", f)
		resp.Body.Close()
		if resp.StatusCode != 400 {
			t.Errorf("%s: got %d", name, resp.StatusCode)
		}
	}
}

func TestGenerateBusy(t *testing.T) {
	svc := &fakeSvc{block: make(chan struct{})}
	srv := newServer(t, svc)
	form := url.Values{"username": {"octocat"}, "repos": {"a"}}
	done := make(chan struct{})
	go func() {
		resp, err := http.PostForm(srv.URL+"/generate", form)
		if err == nil {
			resp.Body.Close()
		}
		close(done)
	}()
	time.Sleep(100 * time.Millisecond)
	resp, err := http.PostForm(srv.URL+"/generate", form)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	close(svc.block)
	<-done
	if resp.StatusCode != 503 {
		t.Fatalf("expected 503 while busy, got %d", resp.StatusCode)
	}
}

func TestExportPDF(t *testing.T) {
	srv := newServer(t, &fakeSvc{})
	body, _ := json.Marshal(map[string]any{"resume": resume.Sample(), "template": "modern", "paper": "letter"})
	resp, err := http.Post(srv.URL+"/api/pdf", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "application/pdf" || !bytes.HasPrefix(b, []byte("%PDF")) {
		t.Fatalf("got %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, `Alex-Developer-resume.pdf`) {
		t.Fatalf("filename: %s", cd)
	}

	bad := map[string]struct {
		ct, body string
		want     int
	}{
		"wrong type":    {"text/plain", "{}", 415},
		"malformed":     {"application/json", "{", 400},
		"unknown field": {"application/json", `{"resume":{"name":"x"},"evil":1}`, 400},
		"no name":       {"application/json", `{"resume":{"name":""}}`, 400},
		"too large":     {"application/json", `{"resume":{"name":"` + strings.Repeat("x", 300<<10) + `"}}`, 400},
	}
	for name, tc := range bad {
		resp, err := http.Post(srv.URL+"/api/pdf", tc.ct, strings.NewReader(tc.body))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != tc.want {
			t.Errorf("%s: got %d want %d", name, resp.StatusCode, tc.want)
		}
	}
}

func TestFileName(t *testing.T) {
	for in, want := range map[string]string{"Jane Doe": "Jane-Doe-resume.pdf", `../../"x"`: "..-..-x-resume.pdf", "": "resume-resume.pdf"} {
		if got := fileName(in); got != want {
			t.Errorf("fileName(%q) = %q, want %q", in, got, want)
		}
	}
}
