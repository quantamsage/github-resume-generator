package safehttp

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAllowlist(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "http://169.254.169.254/latest/meta-data", http.StatusFound)
			return
		}
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	c, err := NewClient(time.Second, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := c.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("allowed host failed: %v", err)
	}
	resp.Body.Close()

	for _, u := range []string{"http://169.254.169.254/", "https://example.com/", "file:///etc/passwd"} {
		if _, err := c.Get(u); !errors.Is(err, ErrHostNotAllowed) {
			t.Errorf("%s: expected ErrHostNotAllowed, got %v", u, err)
		}
	}
	if _, err := c.Get(srv.URL + "/redirect"); !errors.Is(err, ErrHostNotAllowed) {
		t.Errorf("redirect to disallowed host: got %v", err)
	}
}

func TestRejectsPlainHTTPForRemoteHosts(t *testing.T) {
	if _, err := NewClient(time.Second, "http://api.github.com"); err == nil {
		t.Fatal("expected error for non-https remote base URL")
	}
}

func TestReadLimited(t *testing.T) {
	b, err := ReadLimited(strings.NewReader("hello"), 10)
	if err != nil || string(b) != "hello" {
		t.Fatalf("got %q %v", b, err)
	}
	if _, err := ReadLimited(strings.NewReader("hello world"), 5); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("got %v", err)
	}
}
