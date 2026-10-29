package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNormalizeUsername(t *testing.T) {
	ok := map[string]string{
		"octocat":                         "octocat",
		"  @octocat ":                     "octocat",
		"https://github.com/octocat":      "octocat",
		"https://github.com/octocat/":     "octocat",
		"github.com/Octo-Cat":             "Octo-Cat",
		"http://www.github.com/a1?tab=xx": "a1",
	}
	for in, want := range ok {
		got, err := NormalizeUsername(in)
		if err != nil || got != want {
			t.Errorf("NormalizeUsername(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	bad := []string{"", "-octo", "octo-", "oc--to", "octo cat", "https://evil.com/octocat",
		"https://github.com/octocat/repo", "https://github.com.evil.com/x", "../etc/passwd",
		strings.Repeat("a", 40), "ftp://github.com/octocat", "octo/cat"}
	for _, in := range bad {
		if got, err := NormalizeUsername(in); err == nil {
			t.Errorf("NormalizeUsername(%q) = %q; want error", in, got)
		}
	}
}

func TestValidRepoName(t *testing.T) {
	for _, n := range []string{"repo", "my.repo", "a_b-c"} {
		if !ValidRepoName(n) {
			t.Errorf("%q should be valid", n)
		}
	}
	for _, n := range []string{"", ".", "..", "a/b", "a b", "a?b"} {
		if ValidRepoName(n) {
			t.Errorf("%q should be invalid", n)
		}
	}
}

func newTestClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := NewClient(srv.URL, "test-token", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestUserAndHeaders(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" || r.Header.Get("X-GitHub-Api-Version") == "" {
			t.Errorf("missing headers: %v", r.Header)
		}
		if r.URL.Path != "/users/octocat" {
			t.Errorf("path %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"login":"octocat","name":"The Octocat","public_repos":2}`)
	})
	u, err := c.User(context.Background(), "octocat")
	if err != nil || u.Login != "octocat" || u.Name != "The Octocat" {
		t.Fatalf("got %+v, %v", u, err)
	}
}

func TestReposPaginationAndLimit(t *testing.T) {
	var srvURL string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		if page == "" {
			w.Header().Set("Link", fmt.Sprintf(`<%s/users/u/repos?page=2>; rel="next", <%s/users/u/repos?page=2>; rel="last"`, srvURL, srvURL))
			fmt.Fprint(w, `[{"name":"a"},{"name":"b"}]`)
			return
		}
		fmt.Fprint(w, `[{"name":"c"},{"name":"d"}]`)
	})
	srvURL = c.base

	repos, err := c.Repos(context.Background(), "u", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 3 || repos[2].Name != "c" {
		t.Fatalf("got %+v", repos)
	}
}

func TestNextLinkIgnoresForeignHosts(t *testing.T) {
	c := &Client{base: "https://api.github.com"}
	if got := c.nextLink(`<https://evil.example/users/u/repos?page=2>; rel="next"`); got != "" {
		t.Fatalf("foreign next link followed: %q", got)
	}
}

func TestReadmeMissingAndTruncated(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/none/") {
			http.NotFound(w, r)
			return
		}
		if !strings.Contains(r.Header.Get("Accept"), "raw") {
			t.Errorf("README must be requested raw")
		}
		fmt.Fprint(w, strings.Repeat("x", 500))
	})
	s, err := c.Readme(context.Background(), "u", "none", 100)
	if err != nil || s != "" {
		t.Fatalf("missing README: %q, %v", s, err)
	}
	s, err = c.Readme(context.Background(), "u", "big", 100)
	if err != nil || !strings.HasPrefix(s, strings.Repeat("x", 100)) || !strings.Contains(s, "truncated") {
		t.Fatalf("truncation: %q, %v", s, err)
	}
}

func TestErrors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		header map[string]string
		want   error
	}{
		{"not found", 404, nil, ErrNotFound},
		{"rate limited 403", 403, map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": "1900000000"}, ErrRateLimited},
		{"rate limited 429", 429, nil, ErrRateLimited},
		{"server error", 502, nil, ErrUpstream},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				for k, v := range tc.header {
					w.Header().Set(k, v)
				}
				w.WriteHeader(tc.status)
			})
			_, err := c.User(context.Background(), "u")
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestRateLimitResetTime(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", "1900000000")
		w.WriteHeader(403)
	})
	_, err := c.User(context.Background(), "u")
	var rl *RateLimitError
	if !errors.As(err, &rl) || rl.Reset.Unix() != 1900000000 {
		t.Fatalf("got %v", err)
	}
}

func TestMalformedJSON(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{not json`) })
	if _, err := c.User(context.Background(), "u"); !errors.Is(err, ErrUpstream) {
		t.Fatalf("got %v", err)
	}
}

func TestCache(t *testing.T) {
	var calls atomic.Int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		fmt.Fprint(w, `{"login":"u"}`)
	})
	for i := 0; i < 3; i++ {
		if _, err := c.User(context.Background(), "u"); err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("expected 1 upstream call, got %d", calls.Load())
	}
}

func TestContextCancel(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := c.User(ctx, "u"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v", err)
	}
}
