// Package config loads application settings from environment variables.
package config

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all runtime settings. Secrets are kept here and never rendered
// into templates or logs.
type Config struct {
	Addr string

	AnthropicAPIKey  string
	AnthropicModel   string
	AnthropicBaseURL string
	AnthropicEffort  string
	AITimeout        time.Duration

	GitHubToken   string
	GitHubBaseURL string
	GitHubTimeout time.Duration

	MaxRepos        int // repositories listed on the analysis page
	MaxSelected     int // repositories sent to the model
	MaxReadmeBytes  int // README characters kept per repository
	RateLimitPerMin int // requests per minute per client IP
	MaxConcurrentAI int // simultaneous resume generations
	TrustProxy      bool
	LogLevel        string
}

// Load reads configuration from the environment. If a .env file exists in the
// working directory, values from it are applied first without overriding
// variables already set in the environment.
func Load() (Config, error) {
	_ = loadDotEnv(".env")

	c := Config{
		Addr:             env("ADDR", ":8080"),
		AnthropicAPIKey:  strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")),
		AnthropicModel:   env("ANTHROPIC_MODEL", "claude-opus-5-5"),
		AnthropicBaseURL: env("ANTHROPIC_BASE_URL", "https://api.anthropic.com"),
		AnthropicEffort:  env("ANTHROPIC_EFFORT", "medium"),
		GitHubToken:      strings.TrimSpace(os.Getenv("GITHUB_TOKEN")),
		GitHubBaseURL:    env("GITHUB_API_URL", "https://api.github.com"),
		LogLevel:         env("LOG_LEVEL", "info"),
	}

	var err error
	if c.AITimeout, err = envDuration("AI_TIMEOUT", 180*time.Second); err != nil {
		return c, err
	}
	if c.GitHubTimeout, err = envDuration("GITHUB_TIMEOUT", 15*time.Second); err != nil {
		return c, err
	}
	if c.MaxRepos, err = envInt("MAX_REPOS", 30, 1, 100); err != nil {
		return c, err
	}
	if c.MaxSelected, err = envInt("MAX_SELECTED_REPOS", 8, 1, 15); err != nil {
		return c, err
	}
	if c.MaxReadmeBytes, err = envInt("MAX_README_CHARS", 6000, 500, 20000); err != nil {
		return c, err
	}
	if c.RateLimitPerMin, err = envInt("RATE_LIMIT_PER_MINUTE", 30, 1, 10000); err != nil {
		return c, err
	}
	if c.MaxConcurrentAI, err = envInt("MAX_CONCURRENT_GENERATIONS", 4, 1, 64); err != nil {
		return c, err
	}
	c.TrustProxy = env("TRUST_PROXY", "false") == "true"

	switch c.AnthropicEffort {
	case "low", "medium", "high", "xhigh", "max":
	default:
		return c, fmt.Errorf("ANTHROPIC_EFFORT must be one of low, medium, high, xhigh, max")
	}
	return c, nil
}

// AIEnabled reports whether a Claude API key is configured.
func (c Config) AIEnabled() bool { return c.AnthropicAPIKey != "" }

func env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func envInt(key string, def, min, max int) (int, error) {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < min || n > max {
		return 0, fmt.Errorf("%s must be an integer between %d and %d", key, min, max)
	}
	return n, nil
}

func envDuration(key string, def time.Duration) (time.Duration, error) {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration such as 30s", key)
	}
	return d, nil
}

// loadDotEnv applies KEY=VALUE lines from path. Existing variables win.
func loadDotEnv(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		if _, exists := os.LookupEnv(k); !exists {
			os.Setenv(k, v)
		}
	}
	return sc.Err()
}
