// Package handler implements the HTTP endpoints.
package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/judie-paul/resume-generator/internal/github"
	"github.com/judie-paul/resume-generator/internal/pdf"
	"github.com/judie-paul/resume-generator/internal/resume"
)

const (
	maxFormBytes = 64 << 10
	maxJSONBytes = 256 << 10
)

// Generator is the resume workflow the handlers drive.
type Generator interface {
	Analyze(ctx context.Context, username string) (*resume.Analysis, error)
	Generate(ctx context.Context, username string, selected []string, in resume.UserInput) (*resume.Resume, error)
	AIEnabled() bool
}

// Handler serves pages and API endpoints.
type Handler struct {
	svc         Generator
	pages       map[string]*template.Template
	static      fs.FS
	log         *slog.Logger
	genSlots    chan struct{}
	genTimeout  time.Duration
	readTimeout time.Duration
	maxSelected int
}

// Config configures a Handler.
type Config struct {
	MaxConcurrentGenerations int
	GenerateTimeout          time.Duration // whole generate request, including GitHub and AI calls
	AnalyzeTimeout           time.Duration
	MaxSelected              int
}

// New builds a Handler.
func New(svc Generator, pages map[string]*template.Template, static fs.FS, log *slog.Logger, cfg Config) *Handler {
	return &Handler{
		svc: svc, pages: pages, static: static, log: log,
		genSlots:    make(chan struct{}, cfg.MaxConcurrentGenerations),
		genTimeout:  cfg.GenerateTimeout,
		readTimeout: cfg.AnalyzeTimeout,
		maxSelected: cfg.MaxSelected,
	}
}

// Routes registers all endpoints. limited wraps endpoints that call external
// APIs or do heavy work with the rate limiter.
func (h *Handler) Routes(limited func(http.Handler) http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", h.landing)
	mux.Handle("GET /analyze", limited(http.HandlerFunc(h.analyze)))
	mux.Handle("POST /generate", limited(http.HandlerFunc(h.generate)))
	mux.HandleFunc("GET /editor", h.editor)
	mux.Handle("POST /api/pdf", limited(http.HandlerFunc(h.exportPDF)))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte("ok"))
	})
	static := http.StripPrefix("/static/", http.FileServer(http.FS(h.static)))
	mux.Handle("GET /static/", cacheControl(static))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		h.renderError(w, http.StatusNotFound, "Page not found.")
	})
	return mux
}

func cacheControl(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=300")
		next.ServeHTTP(w, r)
	})
}

type landingData struct {
	Error      string
	Username   string
	SampleJSON string
	AIEnabled  bool
}

func (h *Handler) landing(w http.ResponseWriter, r *http.Request) {
	h.render(w, http.StatusOK, "landing", landingData{SampleJSON: mustJSON(resume.Sample()), AIEnabled: h.svc.AIEnabled()})
}

type analyzeData struct {
	Analysis    *resume.Analysis
	Username    string
	AIEnabled   bool
	MaxSelected int
}

func (h *Handler) analyze(w http.ResponseWriter, r *http.Request) {
	input := r.URL.Query().Get("username")
	username, err := github.NormalizeUsername(input)
	if err != nil {
		h.render(w, http.StatusBadRequest, "landing", landingData{
			Error: err.Error(), Username: input, SampleJSON: mustJSON(resume.Sample()), AIEnabled: h.svc.AIEnabled(),
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), h.readTimeout)
	defer cancel()
	a, err := h.svc.Analyze(ctx, username)
	if err != nil {
		status, msg := resume.ErrorMessage(err)
		h.logErr("analyze", err, status)
		h.render(w, status, "landing", landingData{
			Error: msg, Username: input, SampleJSON: mustJSON(resume.Sample()), AIEnabled: h.svc.AIEnabled(),
		})
		return
	}
	h.render(w, http.StatusOK, "analyze", analyzeData{Analysis: a, Username: a.User.Login, AIEnabled: h.svc.AIEnabled(), MaxSelected: h.maxSelected})
}

type editorData struct {
	ResumeJSON string
	Username   string
	Templates  []string
	AIEnabled  bool
}

func (h *Handler) editor(w http.ResponseWriter, r *http.Request) {
	h.render(w, http.StatusOK, "editor", editorData{Templates: pdf.Templates, AIEnabled: h.svc.AIEnabled()})
}

func (h *Handler) generate(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		h.renderError(w, http.StatusBadRequest, "The form could not be read.")
		return
	}
	username, err := github.NormalizeUsername(r.PostForm.Get("username"))
	if err != nil {
		h.renderError(w, http.StatusBadRequest, err.Error())
		return
	}
	selected := r.PostForm["repos"]
	if len(selected) == 0 {
		h.renderError(w, http.StatusBadRequest, "Select at least one repository to include.")
		return
	}
	if len(selected) > h.maxSelected {
		selected = selected[:h.maxSelected]
	}

	in := resume.UserInput{
		Contact: resume.Contact{
			Email:     field(r, "email", 120),
			Phone:     field(r, "phone", 40),
			Location:  field(r, "location", 120),
			LinkedIn:  field(r, "linkedin", 300),
			Website:   field(r, "website", 300),
			Portfolio: field(r, "portfolio", 300),
		},
		Background:     field(r, "background", 3000),
		JobDescription: field(r, "job_description", 4000),
	}
	if in.Contact.Email != "" && !emailRE.MatchString(in.Contact.Email) {
		h.renderError(w, http.StatusBadRequest, "The email address doesn't look valid.")
		return
	}

	select {
	case h.genSlots <- struct{}{}:
		defer func() { <-h.genSlots }()
	default:
		h.renderError(w, http.StatusServiceUnavailable, "The server is busy generating other resumes. Please try again in a minute.")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), h.genTimeout)
	defer cancel()
	res, err := h.svc.Generate(ctx, username, selected, in)
	if err != nil {
		status, msg := resume.ErrorMessage(err)
		h.logErr("generate", err, status)
		h.renderError(w, status, msg)
		return
	}
	h.render(w, http.StatusOK, "editor", editorData{
		ResumeJSON: mustJSON(res), Username: username, Templates: pdf.Templates, AIEnabled: h.svc.AIEnabled(),
	})
}

var emailRE = regexp.MustCompile(`^[^\s@<>"]+@[^\s@<>"]+\.[^\s@<>"]+$`)

type pdfRequest struct {
	Resume   resume.Resume `json:"resume"`
	Template string        `json:"template"`
	Paper    string        `json:"paper"`
}

func (h *Handler) exportPDF(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		jsonError(w, http.StatusUnsupportedMediaType, "Expected a JSON request body.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBytes)
	var req pdfRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "The resume data is invalid or too large.")
		return
	}
	if !pdf.ValidTemplate(req.Template) {
		req.Template = "ats"
	}
	if req.Paper != "letter" {
		req.Paper = "a4"
	}
	if err := resume.Sanitize(&req.Resume); err != nil {
		jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	b, err := pdf.Render(&req.Resume, req.Template, req.Paper)
	if err != nil {
		h.log.Error("pdf render", "err", err)
		jsonError(w, http.StatusInternalServerError, "The PDF could not be generated.")
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="`+fileName(req.Resume.Name)+`"`)
	w.Header().Set("Cache-Control", "no-store")
	w.Write(b)
}

func (h *Handler) render(w http.ResponseWriter, status int, page string, data any) {
	t, ok := h.pages[page]
	if !ok {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		h.log.Error("template", "page", page, "err", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	buf.WriteTo(w)
}

type errorData struct {
	Status  int
	Message string
}

func (h *Handler) renderError(w http.ResponseWriter, status int, msg string) {
	h.render(w, status, "error", errorData{Status: status, Message: msg})
}

func (h *Handler) logErr(op string, err error, status int) {
	level := slog.LevelWarn
	if status >= 500 {
		level = slog.LevelError
	}
	if errors.Is(err, context.Canceled) {
		level = slog.LevelInfo
	}
	h.log.Log(context.Background(), level, op+" failed", "status", status, "err", err)
}

func jsonError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func field(r *http.Request, name string, max int) string {
	return resume.CleanText(r.PostForm.Get(name), max)
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

var unsafeFileChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func fileName(name string) string {
	base := strings.Trim(unsafeFileChars.ReplaceAllString(name, "-"), "-")
	if base == "" {
		base = "resume"
	}
	if len(base) > 60 {
		base = base[:60]
	}
	return base + "-resume.pdf"
}
