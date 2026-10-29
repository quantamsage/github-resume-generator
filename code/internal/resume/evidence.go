package resume

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/judie-paul/resume-generator/internal/github"
)

// RepoEvidence is the sanitized information about one repository that is
// shown to the user and sent to the model.
type RepoEvidence struct {
	Name        string         `json:"name"`
	URL         string         `json:"url"`
	Homepage    string         `json:"homepage,omitempty"`
	Description string         `json:"description,omitempty"`
	Language    string         `json:"primary_language,omitempty"`
	Languages   map[string]int `json:"languages_bytes,omitempty"`
	Topics      []string       `json:"topics,omitempty"`
	Stars       int            `json:"stars"`
	Forks       int            `json:"forks"`
	IsFork      bool           `json:"is_fork"`
	Archived    bool           `json:"archived"`
	License     string         `json:"license,omitempty"`
	CreatedAt   string         `json:"created_at"`
	PushedAt    string         `json:"last_pushed_at"`
	Readme      string         `json:"readme_excerpt,omitempty"`
}

// ScoredRepo pairs a repository with a relevance score for default selection.
type ScoredRepo struct {
	Repo     github.Repo
	Score    float64
	Selected bool
}

// RankRepos scores repositories and marks the top maxSelected as selected.
// Forks and archived repositories are ranked low but still listed so the
// user can opt in. Scoring favors recent, documented, non-trivial projects;
// stars only contribute logarithmically and never imply proficiency.
func RankRepos(repos []github.Repo, maxSelected int, now time.Time) []ScoredRepo {
	out := make([]ScoredRepo, 0, len(repos))
	for _, r := range repos {
		out = append(out, ScoredRepo{Repo: r, Score: score(r, now)})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	for i := range out {
		if i < maxSelected && !out[i].Repo.Fork && out[i].Score > 0 {
			out[i].Selected = true
		}
	}
	return out
}

func score(r github.Repo, now time.Time) float64 {
	s := 0.0
	ageDays := now.Sub(r.PushedAt).Hours() / 24
	s += 4 * math.Exp(-ageDays/365) // recency, decays over ~a year
	if strings.TrimSpace(r.Description) != "" {
		s += 1.5
	}
	if r.Homepage != "" {
		s += 0.5
	}
	if len(r.Topics) > 0 {
		s += 0.5
	}
	if r.Language != "" {
		s += 0.5
	}
	if r.Size > 50 {
		s += 1
	}
	s += 0.5 * math.Log1p(float64(r.Stars+r.Forks))
	if r.Fork {
		s -= 4
	}
	if r.Archived {
		s -= 1
	}
	if isProfileRepo(r) {
		s -= 2
	}
	return s
}

// isProfileRepo detects username/username profile README repositories and dotfiles.
func isProfileRepo(r github.Repo) bool {
	owner, name, _ := strings.Cut(r.FullName, "/")
	return strings.EqualFold(owner, name) || strings.EqualFold(name, "dotfiles") ||
		(strings.HasSuffix(strings.ToLower(name), ".github.io") && r.Size < 50)
}

// BuildEvidence converts repository data into sanitized evidence.
func BuildEvidence(r github.Repo, langs map[string]int, readme string) RepoEvidence {
	ev := RepoEvidence{
		Name:        r.Name,
		URL:         r.HTMLURL,
		Description: CleanText(r.Description, 400),
		Language:    r.Language,
		Languages:   langs,
		Topics:      r.Topics,
		Stars:       r.Stars,
		Forks:       r.Forks,
		IsFork:      r.Fork,
		Archived:    r.Archived,
		CreatedAt:   r.CreatedAt.Format("2006-01-02"),
		PushedAt:    r.PushedAt.Format("2006-01-02"),
		Readme:      CleanReadme(readme),
	}
	if SafeURL(r.Homepage) {
		ev.Homepage = r.Homepage
	}
	if r.License != nil && r.License.SPDXID != "" && r.License.SPDXID != "NOASSERTION" {
		ev.License = r.License.SPDXID
	}
	return ev
}

// Facts returns verified, display-ready facts for a repository.
func (ev RepoEvidence) Facts() []string {
	var f []string
	if langs := TopLanguages(ev.Languages, 3); len(langs) > 0 {
		f = append(f, "Languages: "+strings.Join(langs, ", "))
	} else if ev.Language != "" {
		f = append(f, "Language: "+ev.Language)
	}
	if ev.Stars > 0 {
		f = append(f, fmt.Sprintf("%d GitHub stars", ev.Stars))
	}
	if ev.License != "" {
		f = append(f, "License: "+ev.License)
	}
	if len(ev.PushedAt) >= 4 {
		f = append(f, "Last updated "+ev.PushedAt[:7])
	}
	return f
}

// TopLanguages returns up to n language names ordered by bytes.
func TopLanguages(langs map[string]int, n int) []string {
	type kv struct {
		k string
		v int
	}
	var list []kv
	for k, v := range langs {
		list = append(list, kv{k, v})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].v == list[j].v {
			return list[i].k < list[j].k
		}
		return list[i].v > list[j].v
	})
	var out []string
	for i := 0; i < len(list) && i < n; i++ {
		out = append(out, list[i].k)
	}
	return out
}

// CleanText strips control characters, collapses whitespace and truncates.
func CleanText(s string, max int) string {
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || r == '\r' {
			return ' '
		}
		if unicode.IsControl(r) || r == unicode.ReplacementChar {
			return -1
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > max {
		s = strings.TrimSpace(string(r[:max]))
	}
	return s
}

// CleanReadme removes HTML comments, images, badges and control characters
// from README text while keeping line structure. The result is still
// untrusted data and is only ever sent to the model inside a data block.
func CleanReadme(s string) string {
	for {
		start := strings.Index(s, "<!--")
		if start < 0 {
			break
		}
		end := strings.Index(s[start:], "-->")
		if end < 0 {
			s = s[:start]
			break
		}
		s = s[:start] + s[start+end+3:]
	}
	var b strings.Builder
	blank := 0
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		// Drop image/badge-only lines.
		if strings.HasPrefix(t, "![") || strings.HasPrefix(t, "[![") || strings.HasPrefix(t, "<img") || strings.HasPrefix(t, "<p align") || t == "</p>" {
			continue
		}
		t = strings.Map(func(r rune) rune {
			if r == '\t' {
				return ' '
			}
			if unicode.IsControl(r) {
				return -1
			}
			return r
		}, t)
		if t == "" {
			blank++
			if blank > 1 {
				continue
			}
		} else {
			blank = 0
		}
		b.WriteString(t)
		b.WriteByte('\n')
	}
	return strings.TrimSpace(b.String())
}

// SafeURL reports whether s is an absolute http(s) URL suitable for a link.
func SafeURL(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) > 500 {
		return false
	}
	ls := strings.ToLower(s)
	return (strings.HasPrefix(ls, "https://") || strings.HasPrefix(ls, "http://")) && !strings.ContainsAny(s, " \"'<>`")
}
