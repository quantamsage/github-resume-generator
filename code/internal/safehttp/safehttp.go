// Package safehttp provides an outbound HTTP client that only talks to an
// explicit allowlist of hosts. It is the SSRF guard for every external call.
package safehttp

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrHostNotAllowed is returned when a request targets a host outside the allowlist.
var ErrHostNotAllowed = errors.New("outbound host not allowed")

// ErrTooLarge is returned by ReadLimited when a body exceeds its limit.
var ErrTooLarge = errors.New("response body too large")

type guard struct {
	allowed map[string]bool // host[:port], lower-case
	next    http.RoundTripper
}

func (g *guard) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := g.check(req.URL); err != nil {
		return nil, err
	}
	return g.next.RoundTrip(req)
}

func (g *guard) check(u *url.URL) error {
	if u.Scheme != "https" && u.Scheme != "http" {
		return fmt.Errorf("%w: scheme %q", ErrHostNotAllowed, u.Scheme)
	}
	if u.User != nil {
		return fmt.Errorf("%w: credentials in URL", ErrHostNotAllowed)
	}
	if !g.allowed[strings.ToLower(u.Host)] {
		return fmt.Errorf("%w: %s", ErrHostNotAllowed, u.Host)
	}
	return nil
}

// NewClient returns an HTTP client restricted to the hosts of the given base
// URLs. Plain http is only accepted for loopback hosts (used by tests).
// Redirects are followed only within the allowlist.
func NewClient(timeout time.Duration, baseURLs ...string) (*http.Client, error) {
	g := &guard{allowed: map[string]bool{}, next: http.DefaultTransport.(*http.Transport).Clone()}
	for _, raw := range baseURLs {
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" {
			return nil, fmt.Errorf("invalid base URL %q", raw)
		}
		if u.Scheme != "https" && !isLoopback(u.Hostname()) {
			return nil, fmt.Errorf("base URL %q must use https", raw)
		}
		g.allowed[strings.ToLower(u.Host)] = true
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: g,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return errors.New("too many redirects")
			}
			return g.check(req.URL)
		},
	}, nil
}

func isLoopback(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// ReadLimited reads at most limit bytes from r and returns ErrTooLarge if the
// body is longer.
func ReadLimited(r io.Reader, limit int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return b[:limit], ErrTooLarge
	}
	return b, nil
}
