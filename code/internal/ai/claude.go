// Package ai calls the Claude Messages API over net/http and returns
// structured JSON output.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/judie-paul/resume-generator/internal/safehttp"
)

// Errors callers can match with errors.Is.
var (
	ErrRateLimited = errors.New("the AI service is busy; please try again shortly")
	ErrRefused     = errors.New("the AI service declined to generate this resume")
	ErrTruncated   = errors.New("the AI response was cut off before it finished")
	ErrUpstream    = errors.New("the AI service request failed")
	ErrAuth        = errors.New("the AI service rejected the configured API key")
)

const (
	apiVersion   = "2023-06-01"
	fallbackBeta = "server-side-fallback-2026-07-01"
	maxRespBytes = 4 << 20
	maxTokens    = 16000
)

// Client is a minimal Claude Messages API client.
type Client struct {
	base   string
	key    string
	model  string
	effort string
	http   *http.Client
}

// NewClient creates a client. baseURL is normally https://api.anthropic.com.
func NewClient(baseURL, apiKey, model, effort string, timeout time.Duration) (*Client, error) {
	baseURL = strings.TrimRight(baseURL, "/")
	hc, err := safehttp.NewClient(timeout, baseURL)
	if err != nil {
		return nil, err
	}
	return &Client{base: baseURL, key: apiKey, model: model, effort: effort, http: hc}, nil
}

// Model returns the configured model ID.
func (c *Client) Model() string { return c.model }

type textBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type message struct {
	Role    string      `json:"role"`
	Content []textBlock `json:"content"`
}

type request struct {
	Model        string         `json:"model"`
	MaxTokens    int            `json:"max_tokens"`
	System       string         `json:"system"`
	Messages     []message      `json:"messages"`
	Thinking     map[string]any `json:"thinking"`
	OutputConfig map[string]any `json:"output_config"`
	Fallbacks    string         `json:"fallbacks,omitempty"`
}

type response struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StopReason  string `json:"stop_reason"`
	StopDetails *struct {
		Category    string `json:"category"`
		Explanation string `json:"explanation"`
	} `json:"stop_details"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// Usage reports token consumption for one call.
type Usage struct {
	InputTokens  int
	OutputTokens int
}

// GenerateJSON sends system and user text and asks the model to answer with
// JSON matching schema. It returns the raw JSON text; callers must validate it.
func (c *Client) GenerateJSON(ctx context.Context, system, user string, schema map[string]any) ([]byte, Usage, error) {
	body := request{
		Model:     c.model,
		MaxTokens: maxTokens,
		System:    system,
		Messages:  []message{{Role: "user", Content: []textBlock{{Type: "text", Text: user}}}},
		Thinking:  map[string]any{"type": "adaptive"},
		OutputConfig: map[string]any{
			"effort": c.effort,
			"format": map[string]any{"type": "json_schema", "schema": schema},
		},
	}
	useFallback := supportsDefaultFallback(c.model)
	if useFallback {
		body.Fallbacks = "default"
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, Usage{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/v1/messages", bytes.NewReader(payload))
	if err != nil {
		return nil, Usage{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.key)
	req.Header.Set("anthropic-version", apiVersion)
	if useFallback {
		req.Header.Set("anthropic-beta", fallbackBeta)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, Usage{}, ctx.Err()
		}
		return nil, Usage{}, fmt.Errorf("%w: network error", ErrUpstream)
	}
	defer resp.Body.Close()

	raw, err := safehttp.ReadLimited(resp.Body, maxRespBytes)
	if err != nil {
		return nil, Usage{}, fmt.Errorf("%w: %v", ErrUpstream, err)
	}

	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, Usage{}, ErrAuth
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == 529:
		return nil, Usage{}, ErrRateLimited
	case resp.StatusCode != http.StatusOK:
		return nil, Usage{}, fmt.Errorf("%w: status %d: %s", ErrUpstream, resp.StatusCode, apiErrorMessage(raw))
	}

	var r response
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, Usage{}, fmt.Errorf("%w: invalid response JSON", ErrUpstream)
	}
	usage := Usage{InputTokens: r.Usage.InputTokens, OutputTokens: r.Usage.OutputTokens}

	switch r.StopReason {
	case "refusal":
		return nil, usage, ErrRefused
	case "max_tokens":
		return nil, usage, ErrTruncated
	}

	var text strings.Builder
	for _, b := range r.Content {
		if b.Type == "text" {
			text.WriteString(b.Text)
		}
	}
	if text.Len() == 0 {
		return nil, usage, fmt.Errorf("%w: empty response", ErrUpstream)
	}
	return []byte(text.String()), usage, nil
}

// supportsDefaultFallback reports whether the model accepts fallbacks:"default".
func supportsDefaultFallback(model string) bool {
	switch model {
	case "claude-fable-5-1", "claude-opus-5-5", "claude-opus-5", "claude-sonnet-5-5":
		return true
	}
	return false
}

func apiErrorMessage(raw []byte) string {
	var e struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &e) == nil && e.Error.Message != "" {
		msg := e.Error.Message
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return e.Error.Type + ": " + msg
	}
	return "unexpected response"
}
