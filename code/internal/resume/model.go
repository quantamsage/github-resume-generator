// Package resume holds the resume domain model, repository evidence
// selection, AI-output validation and the fact/inference merge rules.
package resume

// Resume is the document the editor works on and the PDF renderer consumes.
//
// Provenance rules:
//   - Name, contact links and project URLs/facts come from GitHub or the user.
//   - Experience, Education and Certifications come only from the user; the
//     AI never produces them.
//   - Headline, Summary, Skills, project Description/Highlights are
//     AI-written interpretations (flagged in AIWritten) the user can edit.
type Resume struct {
	Name           string          `json:"name"`
	Headline       string          `json:"headline"`
	Summary        string          `json:"summary"`
	Contact        Contact         `json:"contact"`
	Skills         []SkillGroup    `json:"skills"`
	Projects       []Project       `json:"projects"`
	Experience     []Experience    `json:"experience"`
	Education      []Education     `json:"education"`
	Certifications []Certification `json:"certifications"`

	// Inferences lists interpretations the AI made that are not direct facts.
	Inferences []string `json:"inferences"`
	// MissingInfo lists questions for the user about information GitHub can't provide.
	MissingInfo []string `json:"missing_info"`
	// AIWritten is true when headline/summary/skills/project text came from the model.
	AIWritten bool `json:"ai_written"`
}

// Contact holds contact details and profile links.
type Contact struct {
	Email     string `json:"email"`
	Phone     string `json:"phone"`
	Location  string `json:"location"`
	GitHub    string `json:"github"`
	Website   string `json:"website"`
	LinkedIn  string `json:"linkedin"`
	Portfolio string `json:"portfolio"`
}

// SkillGroup is a category of skills, e.g. "Languages".
type SkillGroup struct {
	Category string   `json:"category"`
	Items    []string `json:"items"`
}

// Project is a resume project entry, normally backed by a GitHub repository.
type Project struct {
	Name         string   `json:"name"`
	Repo         string   `json:"repo"` // GitHub repository name, empty for user-added projects
	URL          string   `json:"url"`
	DemoURL      string   `json:"demo_url"`
	Description  string   `json:"description"`
	Highlights   []string `json:"highlights"`
	Technologies []string `json:"technologies"`
	// Facts are verified GitHub metadata (language, stars, dates), set by the server.
	Facts []string `json:"facts"`
}

// Experience is a user-supplied employment entry.
type Experience struct {
	Title      string   `json:"title"`
	Company    string   `json:"company"`
	Location   string   `json:"location"`
	Start      string   `json:"start"`
	End        string   `json:"end"`
	Highlights []string `json:"highlights"`
}

// Education is a user-supplied education entry.
type Education struct {
	Degree      string `json:"degree"`
	Institution string `json:"institution"`
	Start       string `json:"start"`
	End         string `json:"end"`
	Details     string `json:"details"`
}

// Certification is a user-supplied certification entry.
type Certification struct {
	Name   string `json:"name"`
	Issuer string `json:"issuer"`
	Year   string `json:"year"`
	URL    string `json:"url"`
}

// UserInput is information the user supplies that GitHub can't provide.
// Structured experience, education and certifications are added in the editor.
type UserInput struct {
	Contact        Contact
	Background     string // free-text background/achievements, user-stated context for the AI
	JobDescription string // optional target role for tailoring
}
