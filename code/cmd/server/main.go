// Command server runs the GitHub-to-Resume web application.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/judie-paul/resume-generator/internal/ai"
	"github.com/judie-paul/resume-generator/internal/config"
	"github.com/judie-paul/resume-generator/internal/github"
	"github.com/judie-paul/resume-generator/internal/handler"
	"github.com/judie-paul/resume-generator/internal/middleware"
	"github.com/judie-paul/resume-generator/internal/resume"
	"github.com/judie-paul/resume-generator/internal/web"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := newLogger(cfg.LogLevel)

	gh, err := github.NewClient(cfg.GitHubBaseURL, cfg.GitHubToken, cfg.GitHubTimeout)
	if err != nil {
		return fmt.Errorf("github client: %w", err)
	}

	var gen resume.JSONGenerator
	if cfg.AIEnabled() {
		c, err := ai.NewClient(cfg.AnthropicBaseURL, cfg.AnthropicAPIKey, cfg.AnthropicModel, cfg.AnthropicEffort, cfg.AITimeout)
		if err != nil {
			return fmt.Errorf("claude client: %w", err)
		}
		gen = c
		log.Info("AI generation enabled", "model", cfg.AnthropicModel, "effort", cfg.AnthropicEffort)
	} else {
		log.Warn("ANTHROPIC_API_KEY not set; resumes will be facts-only drafts without AI")
	}
	if cfg.GitHubToken == "" {
		log.Info("GITHUB_TOKEN not set; GitHub allows 60 unauthenticated requests per hour from this server")
	}

	svc := resume.NewService(gh, gen, resume.Options{
		MaxRepos:       cfg.MaxRepos,
		MaxSelected:    cfg.MaxSelected,
		MaxReadmeChars: cfg.MaxReadmeBytes,
	}, log)

	pages, err := web.Pages()
	if err != nil {
		return fmt.Errorf("templates: %w", err)
	}
	h := handler.New(svc, pages, web.Static(), log, handler.Config{
		MaxConcurrentGenerations: cfg.MaxConcurrentAI,
		GenerateTimeout:          cfg.AITimeout + 2*cfg.GitHubTimeout,
		AnalyzeTimeout:           3 * cfg.GitHubTimeout,
		MaxSelected:              cfg.MaxSelected,
	})
	limiter := middleware.NewRateLimiter(cfg.RateLimitPerMin, cfg.TrustProxy)

	root := middleware.Chain(h.Routes(limiter.Middleware),
		middleware.Recover(log),
		middleware.Logger(log),
		middleware.SecurityHeaders,
		middleware.SameOrigin,
	)

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           root,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      cfg.AITimeout + 2*cfg.GitHubTimeout + 15*time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		log.Info("server listening", "addr", displayAddr(cfg.Addr), "version", version)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		log.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
	}
	return nil
}

func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl}))
}

func displayAddr(addr string) string {
	if strings.HasPrefix(addr, ":") {
		return "http://localhost" + addr
	}
	return "http://" + addr
}
