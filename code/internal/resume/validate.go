package resume

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Field and list limits applied to AI output and to resumes posted by the browser.
const (
	maxShort     = 120
	maxLine      = 300
	maxParagraph = 1200
	maxList      = 25
	maxProjects  = 15
	maxEntries   = 15
)

// ErrInvalidAIOutput is returned when the model's JSON does not match the schema.
var ErrInvalidAIOutput = errors.New("the AI response did not match the expected resume structure")

// AIOutput is the exact structure the model must return.
type AIOutput struct {
	Headline    string       `json:"headline"`
	Summary     string       `json:"summary"`
	Skills      []SkillGroup `json:"skills"`
	Projects    []AIProject  `json:"projects"`
	Inferences  []string     `json:"inferences"`
	MissingInfo []string     `json:"missing_info"`
}

// AIProject is one project as described by the model. Repo must name one of
// the repositories provided as evidence.
type AIProject struct {
	Repo         string   `json:"repo"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Highlights   []string `json:"highlights"`
	Technologies []string `json:"technologies"`
}

// AIOutputSchema is the JSON schema sent to the Claude API as the structured
// output format. It must stay in sync with AIOutput.
func AIOutputSchema() map[string]any {
	str := map[string]any{"type": "string"}
	strList := map[string]any{"type": "array", "items": str}
	obj := func(props map[string]any) map[string]any {
		req := make([]string, 0, len(props))
		for k := range props {
			req = append(req, k)
		}
		sort.Strings(req) // deterministic request bytes
		return map[string]any{"type": "object", "properties": props, "required": req, "additionalProperties": false}
	}
	return obj(map[string]any{
		"headline": str,
		"summary":  str,
		"skills": map[string]any{"type": "array", "items": obj(map[string]any{
			"category": str,
			"items":    strList,
		})},
		"projects": map[string]any{"type": "array", "items": obj(map[string]any{
			"repo":         str,
			"name":         str,
			"description":  str,
			"highlights":   strList,
			"technologies": strList,
		})},
		"inferences":   strList,
		"missing_info": strList,
	})
}

// ParseAIOutput strictly decodes and validates model output. allowedRepos is
// the set of repository names that were sent as evidence; projects naming any
// other repository are rejected as hallucinated and dropped.
func ParseAIOutput(raw []byte, allowedRepos map[string]RepoEvidence) (AIOutput, error) {
	var out AIOutput
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return out, fmt.Errorf("%w: %v", ErrInvalidAIOutput, err)
	}
	if dec.More() {
		return out, fmt.Errorf("%w: trailing data", ErrInvalidAIOutput)
	}

	out.Headline = CleanText(out.Headline, maxShort)
	out.Summary = CleanText(out.Summary, maxParagraph)
	if out.Headline == "" || out.Summary == "" {
		return out, fmt.Errorf("%w: headline and summary are required", ErrInvalidAIOutput)
	}
	out.Skills = cleanSkills(out.Skills)
	out.Inferences = cleanList(out.Inferences, maxLine, maxList)
	out.MissingInfo = cleanList(out.MissingInfo, maxLine, maxList)

	var projects []AIProject
	seen := map[string]bool{}
	for _, p := range out.Projects {
		repo := strings.TrimSpace(p.Repo)
		key := strings.ToLower(repo)
		if _, ok := lookupRepo(allowedRepos, repo); !ok || seen[key] {
			continue
		}
		seen[key] = true
		p.Repo = repo
		p.Name = CleanText(p.Name, maxShort)
		if p.Name == "" {
			p.Name = repo
		}
		p.Description = CleanText(p.Description, maxParagraph)
		p.Highlights = cleanList(p.Highlights, maxLine, 6)
		p.Technologies = cleanList(p.Technologies, maxShort, 12)
		projects = append(projects, p)
		if len(projects) == maxProjects {
			break
		}
	}
	if len(out.Projects) > 0 && len(projects) == 0 {
		return out, fmt.Errorf("%w: no project referenced a provided repository", ErrInvalidAIOutput)
	}
	out.Projects = projects
	return out, nil
}

func lookupRepo(m map[string]RepoEvidence, name string) (RepoEvidence, bool) {
	if ev, ok := m[name]; ok {
		return ev, true
	}
	for k, ev := range m {
		if strings.EqualFold(k, name) {
			return ev, true
		}
	}
	return RepoEvidence{}, false
}

// Sanitize normalizes a resume received from the browser before rendering:
// it trims and bounds every field, drops empty entries and removes unsafe links.
func Sanitize(r *Resume) error {
	r.Name = CleanText(r.Name, maxShort)
	r.Headline = CleanText(r.Headline, maxShort)
	r.Summary = CleanText(r.Summary, maxParagraph)

	c := &r.Contact
	c.Email = CleanText(c.Email, maxShort)
	c.Phone = CleanText(c.Phone, 40)
	c.Location = CleanText(c.Location, maxShort)
	for _, u := range []*string{&c.GitHub, &c.Website, &c.LinkedIn, &c.Portfolio} {
		*u = cleanURL(*u)
	}

	r.Skills = cleanSkills(r.Skills)

	if len(r.Projects) > maxProjects {
		r.Projects = r.Projects[:maxProjects]
	}
	projects := r.Projects[:0]
	for _, p := range r.Projects {
		p.Name = CleanText(p.Name, maxShort)
		p.Repo = CleanText(p.Repo, 100)
		p.URL = cleanURL(p.URL)
		p.DemoURL = cleanURL(p.DemoURL)
		p.Description = CleanText(p.Description, maxParagraph)
		p.Highlights = cleanList(p.Highlights, maxLine, 8)
		p.Technologies = cleanList(p.Technologies, maxShort, 15)
		p.Facts = cleanList(p.Facts, maxShort, 6)
		if p.Name != "" {
			projects = append(projects, p)
		}
	}
	r.Projects = projects

	if len(r.Experience) > maxEntries {
		r.Experience = r.Experience[:maxEntries]
	}
	exp := r.Experience[:0]
	for _, e := range r.Experience {
		e.Title = CleanText(e.Title, maxShort)
		e.Company = CleanText(e.Company, maxShort)
		e.Location = CleanText(e.Location, maxShort)
		e.Start = CleanText(e.Start, 30)
		e.End = CleanText(e.End, 30)
		e.Highlights = cleanList(e.Highlights, maxLine, 8)
		if e.Title != "" || e.Company != "" {
			exp = append(exp, e)
		}
	}
	r.Experience = exp

	if len(r.Education) > maxEntries {
		r.Education = r.Education[:maxEntries]
	}
	edu := r.Education[:0]
	for _, e := range r.Education {
		e.Degree = CleanText(e.Degree, maxShort)
		e.Institution = CleanText(e.Institution, maxShort)
		e.Start = CleanText(e.Start, 30)
		e.End = CleanText(e.End, 30)
		e.Details = CleanText(e.Details, maxLine)
		if e.Degree != "" || e.Institution != "" {
			edu = append(edu, e)
		}
	}
	r.Education = edu

	if len(r.Certifications) > maxEntries {
		r.Certifications = r.Certifications[:maxEntries]
	}
	certs := r.Certifications[:0]
	for _, c := range r.Certifications {
		c.Name = CleanText(c.Name, maxShort)
		c.Issuer = CleanText(c.Issuer, maxShort)
		c.Year = CleanText(c.Year, 20)
		c.URL = cleanURL(c.URL)
		if c.Name != "" {
			certs = append(certs, c)
		}
	}
	r.Certifications = certs

	r.Inferences = cleanList(r.Inferences, maxLine, maxList)
	r.MissingInfo = cleanList(r.MissingInfo, maxLine, maxList)

	if r.Name == "" {
		return errors.New("the resume needs a name")
	}
	return nil
}

func cleanSkills(groups []SkillGroup) []SkillGroup {
	var out []SkillGroup
	for _, g := range groups {
		g.Category = CleanText(g.Category, 60)
		g.Items = cleanList(g.Items, 60, 30)
		if g.Category != "" && len(g.Items) > 0 {
			out = append(out, g)
		}
		if len(out) == 10 {
			break
		}
	}
	return out
}

func cleanList(items []string, maxLen, maxItems int) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, it := range items {
		it = CleanText(it, maxLen)
		k := strings.ToLower(it)
		if it == "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, it)
		if len(out) == maxItems {
			break
		}
	}
	return out
}

func cleanURL(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if !strings.Contains(s, "://") && strings.Contains(s, ".") && !strings.ContainsAny(s, " <>\"'") {
		s = "https://" + s
	}
	if !SafeURL(s) {
		return ""
	}
	return s
}
