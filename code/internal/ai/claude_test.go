package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newTestClient(t *testing.T, model string, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := NewClient(srv.URL, "sk-test", model, "medium", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

var schema = map[string]any{"type": "object"}

func TestGenerateJSONRequestShape(t *testing.T) {
	c := newTestClient(t, "claude-opus-5-5", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" || r.Method != http.MethodPost {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "sk-test" || r.Header.Get("anthropic-version") == "" {
			t.Errorf("missing auth/version headers")
		}
		if r.Header.Get("anthropic-beta") != fallbackBeta {
			t.Errorf("fallback beta header missing")
		}
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatal(err)
		}
		if req["model"] != "claude-opus-5-5" || req["fallbacks"] != "default" || req["system"] != "sys" {
			t.Errorf("bad request: %s", body)
		}
		oc := req["output_config"].(map[string]any)
		if oc["effort"] != "medium" || oc["format"].(map[string]any)["type"] != "json_schema" {
			t.Errorf("bad output_config: %v", oc)
		}
		if req["thinking"].(map[string]any)["type"] != "adaptive" {
			t.Errorf("bad thinking: %v", req["thinking"])
		}
		fmt.Fprint(w, `{"content":[{"type":"thinking","thinking":""},{"type":"text","text":"{\"ok\":true}"}],
			"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":5}}`)
	})
	out, usage, err := c.GenerateJSON(context.Background(), "sys", "user", schema)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"ok":true}` || usage.InputTokens != 10 {
		t.Fatalf("got %s %+v", out, usage)
	}
}

func TestNoFallbackForOtherModels(t *testing.T) {
	c := newTestClient(t, "claude-haiku-5-5", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Header.Get("anthropic-beta") != "" || strings.Contains(string(body), "fallbacks") {
			t.Errorf("fallbacks sent for unsupported model")
		}
		fmt.Fprint(w, `{"content":[{"type":"text","text":"{}"}],"stop_reason":"end_turn"}`)
	})
	if _, _, err := c.GenerateJSON(context.Background(), "s", "u", schema); err != nil {
		t.Fatal(err)
	}
}

func TestErrors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   error
	}{
		{"refusal", 200, `{"content":[],"stop_reason":"refusal","stop_details":{"category":"cyber"}}`, ErrRefused},
		{"max tokens", 200, `{"content":[{"type":"text","text":"{\"a\":"}],"stop_reason":"max_tokens"}`, ErrTruncated},
		{"empty", 200, `{"content":[],"stop_reason":"end_turn"}`, ErrUpstream},
		{"malformed", 200, `{"content":`, ErrUpstream},
		{"rate limit", 429, `{"type":"error","error":{"type":"rate_limit_error","message":"slow"}}`, ErrRateLimited},
		{"overloaded", 529, `{"type":"error","error":{"type":"overloaded_error","message":"busy"}}`, ErrRateLimited},
		{"auth", 401, `{"type":"error","error":{"type":"authentication_error","message":"bad key"}}`, ErrAuth},
		{"bad request", 400, `{"type":"error","error":{"type":"invalid_request_error","message":"nope"}}`, ErrUpstream},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t, "claude-opus-5-5", func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			})
			_, _, err := c.GenerateJSON(context.Background(), "s", "u", schema)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
			if err != nil && strings.Contains(err.Error(), "sk-test") {
				t.Fatal("error leaked API key")
			}
		})
	}
}

func TestTimeout(t *testing.T) {
	c := newTestClient(t, "claude-opus-5-5", func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body) // lets the server notice the client hanging up
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, _, err := c.GenerateJSON(ctx, "s", "u", schema); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v", err)
	}
}
