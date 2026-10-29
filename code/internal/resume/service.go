package resume

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/judie-paul/resume-generator/internal/ai"
	"github.com/judie-paul/resume-generator/internal/github"
)

// RepoSource is the GitHub data the service needs.
type RepoSource interface {
	User(ctx context.Context, username string) (*github.User, error)
	Repos(ctx context.Context, username string, limit int) ([]github.Repo, error)
	Readme(ctx context.Context, owner, repo string, maxChars int) (string, error)
	Languages(ctx context.Context, owner, repo string) (map[string]int, error)
}

// JSONGenerator produces schema-constrained JSON from a prompt.
type JSONGenerator interface {
	GenerateJSON(ctx context.Context, system, user string, schema map[string]any) ([]byte, ai.Usage, error)
}

// ErrNoRepos is returned when nothing usable was selected.
var ErrNoRepos = errors.New("select at least one repository")

// Options bounds how much data is fetched and analyzed.
type Options struct {
	MaxRepos       int
	MaxSelected    int
	MaxReadmeChars int
	FetchWorkers   int
}

// Service runs the analysis and generation workflow.
type Service struct {
	gh   RepoSource
	ai   JSONGenerator // nil disables AI; a facts-only draft is produced instead
	opts Options
	log  *slog.Logger
	now  func() time.Time
}

// NewService builds a Service. gen may be nil.
func NewService(gh RepoSource, gen JSONGenerator, opts Options, log *slog.Logger) *Service {
	if opts.FetchWorkers <= 0 {
		opts.FetchWorkers = 4
	}
	return &Service{gh: gh, ai: gen, opts: opts, log: log, now: time.Now}
}

// AIEnabled reports whether a model is configured.
func (s *Service) AIEnabled() bool { return s.ai != nil }

// Analysis is the data shown on the repository selection page.
type Analysis struct {
	User        *github.User
	Repos       []ScoredRepo
	MaxSelected int
}

// Analyze fetches the profile and ranks its repositories.
func (s *Service) Analyze(ctx context.Context, username string) (*Analysis, error) {
	u, err := s.gh.User(ctx, username)
	if err != nil {
		return nil, err
	}
	repos, err := s.gh.Repos(ctx, u.Login, s.opts.MaxRepos)
	if err != nil {
		return nil, err
	}
	return &Analysis{User: u, Repos: RankRepos(repos, s.opts.MaxSelected, s.now()), MaxSelected: s.opts.MaxSelected}, nil
}

// Generate fetches evidence for the selected repositories and produces a resume.
func (s *Service) Generate(ctx context.Context, username string, selected []string, in UserInput) (*Resume, error) {
	u, err := s.gh.User(ctx, username)
	if err != nil {
		return nil, err
	}
	repos, err := s.gh.Repos(ctx, u.Login, s.opts.MaxRepos)
	if err != nil {
		return nil, err
	}

	want := map[string]bool{}
	for _, name := range selected {
		if github.ValidRepoName(name) {
			want[strings.ToLower(name)] = true
		}
	}
	var chosen []github.Repo
	for _, r := range repos {
		if want[strings.ToLower(r.Name)] {
			chosen = append(chosen, r)
		}
		if len(chosen) == s.opts.MaxSelected {
			break
		}
	}
	if len(chosen) == 0 {
		return nil, ErrNoRepos
	}

	evidence, err := s.collectEvidence(ctx, u.Login, chosen)
	if err != nil {
		return nil, err
	}

	if s.ai == nil {
		return BuildDraft(u, evidence, in), nil
	}

	profile := profileEvidence{
		Login:       u.Login,
		Name:        CleanText(u.Name, maxShort),
		Bio:         CleanText(u.Bio, 300),
		Company:     CleanText(u.Company, maxShort),
		Location:    CleanText(u.Location, maxShort),
		PublicRepos: u.PublicRepos,
		MemberSince: u.CreatedAt.Format("2006"),
	}
	prompt, err := BuildUserPrompt(profile, evidence, in)
	if err != nil {
		return nil, err
	}

	start := s.now()
	raw, usage, err := s.ai.GenerateJSON(ctx, systemPrompt, prompt, AIOutputSchema())
	s.log.Info("ai generation", "user", u.Login, "repos", len(evidence),
		"input_tokens", usage.InputTokens, "output_tokens", usage.OutputTokens,
		"duration", time.Since(start).Round(time.Millisecond), "ok", err == nil)
	if err != nil {
		return nil, err
	}

	byName := map[string]RepoEvidence{}
	for _, ev := range evidence {
		byName[ev.Name] = ev
	}
	out, err := ParseAIOutput(raw, byName)
	if err != nil {
		s.log.Warn("invalid ai output", "user", u.Login, "err", err)
		return nil, err
	}
	return Merge(u, out, byName, in), nil
}

// collectEvidence fetches README and language data with bounded concurrency.
// A failed README or language fetch degrades that repository; it does not fail
// the whole request unless GitHub rate limits us.
func (s *Service) collectEvidence(ctx context.Context, owner string, repos []github.Repo) ([]RepoEvidence, error) {
	out := make([]RepoEvidence, len(repos))
	errs := make([]error, len(repos))
	sem := make(chan struct{}, s.opts.FetchWorkers)
	var wg sync.WaitGroup
	for i, r := range repos {
		wg.Add(1)
		go func(i int, r github.Repo) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			readme, err := s.gh.Readme(ctx, owner, r.Name, s.opts.MaxReadmeChars)
			if err != nil {
				errs[i] = err
				readme = ""
			}
			langs, err := s.gh.Languages(ctx, owner, r.Name)
			if err != nil {
				errs[i] = err
				langs = nil
			}
			out[i] = BuildEvidence(r, langs, readme)
		}(i, r)
	}
	wg.Wait()

	for _, err := range errs {
		if errors.Is(err, github.ErrRateLimited) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		if err != nil {
			s.log.Warn("partial repository data", "owner", owner, "err", err)
		}
	}
	return out, nil
}

// Merge combines validated AI output with verified GitHub facts and user input.
func Merge(u *github.User, out AIOutput, byName map[string]RepoEvidence, in UserInput) *Resume {
	r := baseResume(u, in)
	r.Headline = out.Headline
	r.Summary = out.Summary
	r.Skills = out.Skills
	r.Inferences = out.Inferences
	r.MissingInfo = out.MissingInfo
	r.AIWritten = true
	for _, p := range out.Projects {
		ev, _ := lookupRepo(byName, p.Repo)
		r.Projects = append(r.Projects, Project{
			Name:         p.Name,
			Repo:         ev.Name,
			URL:          ev.URL,
			DemoURL:      ev.Homepage,
			Description:  p.Description,
			Highlights:   p.Highlights,
			Technologies: p.Technologies,
			Facts:        ev.Facts(),
		})
	}
	return r
}

// BuildDraft creates a facts-only resume without AI, used when no API key is
// configured. Text comes directly from GitHub metadata.
func BuildDraft(u *github.User, evidence []RepoEvidence, in UserInput) *Resume {
	r := baseResume(u, in)
	r.Headline = "Software Developer"
	r.Summary = CleanText(u.Bio, maxParagraph)

	langCount := map[string]int{}
	topicSet := map[string]bool{}
	var topics []string
	for _, ev := range evidence {
		for lang, n := range ev.Languages {
			langCount[lang] += n
		}
		if len(ev.Languages) == 0 && ev.Language != "" {
			langCount[ev.Language]++
		}
		for _, t := range ev.Topics {
			if !topicSet[t] {
				topicSet[t] = true
				topics = append(topics, t)
			}
		}
		desc := ev.Description
		if desc == "" {
			desc = "Public repository on GitHub."
		}
		r.Projects = append(r.Projects, Project{
			Name:         ev.Name,
			Repo:         ev.Name,
			URL:          ev.URL,
			DemoURL:      ev.Homepage,
			Description:  desc,
			Technologies: TopLanguages(ev.Languages, 4),
			Facts:        ev.Facts(),
		})
	}
	if langs := TopLanguages(langCount, 8); len(langs) > 0 {
		r.Skills = append(r.Skills, SkillGroup{Category: "Languages", Items: langs})
	}
	if len(topics) > 0 {
		r.Skills = append(r.Skills, SkillGroup{Category: "Topics", Items: cleanList(topics, 60, 12)})
	}
	r.MissingInfo = []string{
		"Write a headline and professional summary (AI generation is disabled because ANTHROPIC_API_KEY is not set).",
		"Add work experience, education and certifications if you have them.",
		"Add highlights describing what each project does and what you built.",
	}
	return r
}

func baseResume(u *github.User, in UserInput) *Resume {
	name := CleanText(u.Name, maxShort)
	if name == "" {
		name = u.Login
	}
	c := in.Contact
	if c.Email == "" {
		c.Email = CleanText(u.Email, maxShort)
	}
	if c.Location == "" {
		c.Location = CleanText(u.Location, maxShort)
	}
	if c.GitHub == "" {
		c.GitHub = u.HTMLURL
	}
	if c.Website == "" && u.Blog != "" {
		c.Website = u.Blog
	}
	r := &Resume{Name: name, Contact: c, Skills: []SkillGroup{}, Projects: []Project{},
		Experience: []Experience{}, Education: []Education{}, Certifications: []Certification{},
		Inferences: []string{}, MissingInfo: []string{}}
	_ = Sanitize(r) // normalizes contact URLs; name is never empty here
	return r
}

// ErrorMessage maps errors to safe, user-facing text and an HTTP status.
func ErrorMessage(err error) (int, string) {
	var rl *github.RateLimitError
	switch {
	case errors.As(err, &rl):
		msg := "GitHub's API rate limit has been reached. Please try again later"
		if !rl.Reset.IsZero() {
			mins := int(time.Until(rl.Reset).Minutes()) + 1
			msg = fmt.Sprintf("%s (in about %d minute(s))", msg, mins)
		}
		return 429, msg + "."
	case errors.Is(err, github.ErrNotFound):
		return 404, "That GitHub user could not be found."
	case errors.Is(err, github.ErrInvalidUsername):
		return 400, github.ErrInvalidUsername.Error()
	case errors.Is(err, ErrNoRepos):
		return 400, "Select at least one repository to include."
	case errors.Is(err, github.ErrUpstream):
		return 502, "GitHub could not be reached. Please try again."
	case errors.Is(err, ai.ErrRateLimited):
		return 503, ai.ErrRateLimited.Error()
	case errors.Is(err, ai.ErrRefused), errors.Is(err, ai.ErrTruncated), errors.Is(err, ErrInvalidAIOutput):
		return 502, "The AI response could not be used. Please try again, or select fewer repositories."
	case errors.Is(err, ai.ErrAuth):
		return 500, "The server's AI configuration is invalid. Please contact the operator."
	case errors.Is(err, ai.ErrUpstream):
		return 502, "The AI service could not be reached. Please try again."
	case errors.Is(err, context.DeadlineExceeded):
		return 504, "The request took too long. Please try again with fewer repositories."
	case errors.Is(err, context.Canceled):
		return 499, "The request was cancelled."
	}
	return 500, "Something went wrong. Please try again."
}
