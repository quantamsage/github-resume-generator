package middleware

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

var ok = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

func TestRateLimiter(t *testing.T) {
	l := NewRateLimiter(60, false) // 1/sec, burst 20
	now := time.Unix(0, 0)
	l.now = func() time.Time { return now }
	for i := 0; i < 20; i++ {
		if !l.Allow("a") {
			t.Fatalf("request %d denied within burst", i)
		}
	}
	if l.Allow("a") {
		t.Fatal("burst exceeded but allowed")
	}
	if !l.Allow("b") {
		t.Fatal("other clients must have their own bucket")
	}
	now = now.Add(2 * time.Second)
	if !l.Allow("a") {
		t.Fatal("tokens should refill over time")
	}
}

func TestRateLimiterMiddleware(t *testing.T) {
	h := NewRateLimiter(1, false).Middleware(ok)
	var last int
	for i := 0; i < 10; i++ {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
		last = rec.Code
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", last)
	}
}

func TestClientIP(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Set("X-Forwarded-For", "1.1.1.1, 2.2.2.2")
	if got := ClientIP(r, false); got != "10.0.0.1" {
		t.Errorf("untrusted proxy: got %s", got)
	}
	if got := ClientIP(r, true); got != "2.2.2.2" {
		t.Errorf("trusted proxy: got %s", got)
	}
}

func TestSameOrigin(t *testing.T) {
	h := SameOrigin(ok)
	cases := []struct {
		method, origin, site string
		want                 int
	}{
		{"GET", "https://evil.com", "cross-site", 200},
		{"POST", "", "", 200},
		{"POST", "http://example.com", "same-origin", 200},
		{"POST", "https://evil.com", "", 403},
		{"POST", "", "cross-site", 403},
	}
	for _, tc := range cases {
		r := httptest.NewRequest(tc.method, "http://example.com/generate", nil)
		if tc.origin != "" {
			r.Header.Set("Origin", tc.origin)
		}
		if tc.site != "" {
			r.Header.Set("Sec-Fetch-Site", tc.site)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != tc.want {
			t.Errorf("%+v: got %d", tc, rec.Code)
		}
	}
}

func TestRecoverAndHeaders(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h := Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { panic("boom") }),
		Recover(log), Logger(log), SecurityHeaders)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 500 {
		t.Fatalf("got %d", rec.Code)
	}
	if rec.Header().Get("Content-Security-Policy") == "" || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("security headers missing")
	}
}
