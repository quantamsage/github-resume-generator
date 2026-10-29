// Package middleware contains HTTP middleware for logging, panic recovery,
// security headers, same-origin enforcement and per-client rate limiting.
package middleware

import (
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Chain applies middleware so the first argument is the outermost.
func Chain(h http.Handler, mws ...func(http.Handler) http.Handler) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

// Logger logs one line per request. Query strings and bodies are not logged.
func Logger(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w}
			next.ServeHTTP(rec, r)
			if rec.status == 0 {
				rec.status = http.StatusOK
			}
			log.Info("request", "method", r.Method, "path", r.URL.Path, "status", rec.status,
				"bytes", rec.bytes, "duration", time.Since(start).Round(time.Millisecond))
		})
	}
}

// Recover turns panics into 500 responses.
func Recover(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if v := recover(); v != nil {
					if v == http.ErrAbortHandler {
						panic(v)
					}
					log.Error("panic", "path", r.URL.Path, "panic", v)
					http.Error(w, "Internal server error", http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// SecurityHeaders sets a strict CSP and related headers. Scripts and styles
// load only from this origin; avatars may load from GitHub's avatar CDN.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data: https://avatars.githubusercontent.com; "+
			"script-src 'self'; style-src 'self'; object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		next.ServeHTTP(w, r)
	})
}

// SameOrigin rejects state-changing requests that come from another site,
// a lightweight CSRF defence that needs no tokens.
func SameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
			http.Error(w, "Cross-site request rejected", http.StatusForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != "null" {
			u, err := url.Parse(origin)
			if err != nil || !strings.EqualFold(u.Host, r.Host) {
				http.Error(w, "Cross-origin request rejected", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// RateLimiter is a per-client token bucket limiter.
type RateLimiter struct {
	mu         sync.Mutex
	buckets    map[string]*bucket
	rate       float64 // tokens per second
	burst      float64
	trustProxy bool
	now        func() time.Time
}

type bucket struct {
	tokens float64
	last   time.Time
}

// NewRateLimiter allows perMinute requests per client with a burst of perMinute/3 (min 5).
func NewRateLimiter(perMinute int, trustProxy bool) *RateLimiter {
	burst := float64(perMinute) / 3
	if burst < 5 {
		burst = 5
	}
	return &RateLimiter{buckets: map[string]*bucket{}, rate: float64(perMinute) / 60, burst: burst,
		trustProxy: trustProxy, now: time.Now}
}

// Allow reports whether the client may make another request.
func (l *RateLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	b, ok := l.buckets[key]
	if !ok {
		if len(l.buckets) > 10000 {
			l.evict(now)
		}
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}
	b.tokens += now.Sub(b.last).Seconds() * l.rate
	if b.tokens > l.burst {
		b.tokens = l.burst
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func (l *RateLimiter) evict(now time.Time) {
	for k, b := range l.buckets {
		if now.Sub(b.last) > 10*time.Minute {
			delete(l.buckets, k)
		}
	}
}

// Middleware wraps a handler with the limiter.
func (l *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !l.Allow(ClientIP(r, l.trustProxy)) {
			w.Header().Set("Retry-After", "60")
			http.Error(w, "Too many requests. Please wait a minute and try again.", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ClientIP returns the client address. X-Forwarded-For is only honored when
// the app runs behind a trusted reverse proxy.
func ClientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			return strings.TrimSpace(parts[len(parts)-1])
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
