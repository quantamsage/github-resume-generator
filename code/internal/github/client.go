// Package github is a small read-only client for the GitHub REST API.
package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/judie-paul/resume-generator/internal/safehttp"
)

// Errors callers can match with errors.Is.
var (
	ErrNotFound    = errors.New("GitHub user or repository not found")
	ErrRateLimited = errors.New("GitHub API rate limit reached")
	ErrUpstream    = errors.New("GitHub API request failed")
)

// RateLimitError carries the reset time when GitHub rate limits us.
type RateLimitError struct{ Reset time.Time }

func (e *RateLimitError) Error() string {
	if e.Reset.IsZero() {
		return ErrRateLimited.Error()
	}
	return fmt.Sprintf("%s; resets at %s", ErrRateLimited, e.Reset.UTC().Format(time.RFC3339))
}
func (e *RateLimitError) Unwrap() error { return ErrRateLimited }

const (
	maxJSONBytes   = 2 << 20   // 2 MiB per API response
	maxReadmeBytes = 256 << 10 // raw README bytes read before truncation
	maxPages       = 3         // pagination cap for repository listing
	cacheTTL       = 10 * time.Minute
	cacheMaxItems  = 500
)

// Client talks to the GitHub REST API. It is safe for concurrent use.
type Client struct {
	base  string
	token string
	http  *http.Client

	mu    sync.Mutex
	cache map[string]cacheEntry
}

type cacheEntry struct {
	body    []byte
	next    string
	expires time.Time
}

// NewClient creates a client for baseURL (normally https://api.github.com).
// token is optional; it raises the rate limit from 60 to 5000 requests/hour.
func NewClient(baseURL, token string, timeout time.Duration) (*Client, error) {
	baseURL = strings.TrimRight(baseURL, "/")
	hc, err := safehttp.NewClient(timeout, baseURL)
	if err != nil {
		return nil, err
	}
	return &Client{base: baseURL, token: token, http: hc, cache: map[string]cacheEntry{}}, nil
}

// User fetches a public profile.
func (c *Client) User(ctx context.Context, username string) (*User, error) {
	var u User
	if _, err := c.getJSON(ctx, "/users/"+url.PathEscape(username), &u); err != nil {
		return nil, err
	}
	return &u, nil
}

// Repos lists up to limit public repositories owned by username, most
// recently pushed first, following pagination.
func (c *Client) Repos(ctx context.Context, username string, limit int) ([]Repo, error) {
	var all []Repo
	path := "/users/" + url.PathEscape(username) + "/repos?type=owner&sort=pushed&per_page=100"
	for page := 0; page < maxPages && path != "" && len(all) < limit; page++ {
		var batch []Repo
		next, err := c.getJSON(ctx, path, &batch)
		if err != nil {
			return nil, err
		}
		all = append(all, batch...)
		path = next
	}
	if len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}

// Readme returns the raw README text, truncated to maxChars. A missing README
// returns "", nil.
func (c *Client) Readme(ctx context.Context, owner, repo string, maxChars int) (string, error) {
	path := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo) + "/readme"
	body, _, err := c.get(ctx, path, "application/vnd.github.raw+json", maxReadmeBytes, true)
	if errors.Is(err, ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return truncateRunes(string(body), maxChars), nil
}

// Languages returns bytes of code per language for a repository.
func (c *Client) Languages(ctx context.Context, owner, repo string) (map[string]int, error) {
	langs := map[string]int{}
	path := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo) + "/languages"
	if _, err := c.getJSON(ctx, path, &langs); err != nil {
		if errors.Is(err, ErrNotFound) {
			return map[string]int{}, nil
		}
		return nil, err
	}
	return langs, nil
}

func (c *Client) getJSON(ctx context.Context, path string, v any) (next string, err error) {
	body, next, err := c.get(ctx, path, "application/vnd.github+json", maxJSONBytes, false)
	if err != nil {
		return "", err
	}
	if err := json.Unmarshal(body, v); err != nil {
		return "", fmt.Errorf("%w: invalid JSON response", ErrUpstream)
	}
	return next, nil
}

// get performs a cached GET. path is relative to the API base and may come
// from a Link header (in which case it has already been made relative).
func (c *Client) get(ctx context.Context, path, accept string, limit int64, allowTruncate bool) ([]byte, string, error) {
	key := accept + " " + path
	if body, next, ok := c.fromCache(key); ok {
		return body, next, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "resume-generator")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, "", ctx.Err()
		}
		return nil, "", fmt.Errorf("%w: %v", ErrUpstream, errors.Unwrap(err))
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, "", ErrNotFound
	case resp.StatusCode == http.StatusTooManyRequests,
		resp.StatusCode == http.StatusForbidden && resp.Header.Get("X-RateLimit-Remaining") == "0":
		return nil, "", &RateLimitError{Reset: parseReset(resp.Header.Get("X-RateLimit-Reset"))}
	case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnavailableForLegalReasons:
		return nil, "", ErrNotFound // blocked or inaccessible: treat like missing
	case resp.StatusCode != http.StatusOK:
		return nil, "", fmt.Errorf("%w: status %d", ErrUpstream, resp.StatusCode)
	}

	body, err := safehttp.ReadLimited(resp.Body, limit)
	if err != nil && !(allowTruncate && errors.Is(err, safehttp.ErrTooLarge)) {
		return nil, "", fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	next := c.nextLink(resp.Header.Get("Link"))
	c.toCache(key, body, next)
	return body, next, nil
}

var linkNextRE = regexp.MustCompile(`<([^>]+)>;\s*rel="next"`)

// nextLink extracts the rel="next" URL and returns it relative to the base,
// or "" if absent or pointing elsewhere.
func (c *Client) nextLink(h string) string {
	m := linkNextRE.FindStringSubmatch(h)
	if m == nil || !strings.HasPrefix(m[1], c.base+"/") {
		return ""
	}
	return strings.TrimPrefix(m[1], c.base)
}

func parseReset(v string) time.Time {
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(n, 0)
}

func (c *Client) fromCache(key string) ([]byte, string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.cache[key]
	if !ok || time.Now().After(e.expires) {
		return nil, "", false
	}
	return e.body, e.next, true
}

func (c *Client) toCache(key string, body []byte, next string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.cache) >= cacheMaxItems {
		now := time.Now()
		for k, e := range c.cache {
			if now.After(e.expires) {
				delete(c.cache, k)
			}
		}
		if len(c.cache) >= cacheMaxItems {
			c.cache = map[string]cacheEntry{}
		}
	}
	c.cache[key] = cacheEntry{body: body, next: next, expires: time.Now().Add(cacheTTL)}
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "\n…[truncated]"
}
