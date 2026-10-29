package resume

// Sample returns a fictional resume used for the landing page preview and tests.
func Sample() *Resume {
	return &Resume{
		Name:     "Alex Developer",
		Headline: "Backend Engineer — Go, PostgreSQL, Cloud Infrastructure",
		Summary: "Backend-focused developer whose public work centers on Go services, REST APIs and developer tooling. " +
			"Projects show attention to testing, documentation and container-based deployment.",
		Contact: Contact{
			Email:    "alex@example.com",
			Location: "Nairobi, Kenya",
			GitHub:   "https://github.com/example",
			Website:  "https://example.dev",
		},
		Skills: []SkillGroup{
			{Category: "Languages", Items: []string{"Go", "TypeScript", "SQL", "Bash"}},
			{Category: "Frameworks & Libraries", Items: []string{"net/http", "React", "gRPC"}},
			{Category: "Databases", Items: []string{"PostgreSQL", "SQLite", "Redis"}},
			{Category: "Tools & Platforms", Items: []string{"Docker", "GitHub Actions", "Linux"}},
		},
		Projects: []Project{
			{
				Name: "taskqueue", Repo: "taskqueue", URL: "https://github.com/example/taskqueue",
				Description:  "A lightweight background job queue for Go services backed by PostgreSQL.",
				Highlights:   []string{"Implements retries with exponential backoff and dead-letter handling", "Uses SKIP LOCKED row locking so multiple workers can poll safely", "Includes integration tests that run against a containerized database"},
				Technologies: []string{"Go", "PostgreSQL", "Docker"},
				Facts:        []string{"Languages: Go, Shell", "Last updated 2026-08"},
			},
			{
				Name: "budget-dashboard", Repo: "budget-dashboard", URL: "https://github.com/example/budget-dashboard",
				DemoURL:      "https://budget.example.dev",
				Description:  "A personal finance dashboard with CSV import and category charts.",
				Highlights:   []string{"Builds a REST API in Go consumed by a React front end", "Parses bank CSV exports and categorizes transactions with user-defined rules"},
				Technologies: []string{"Go", "React", "TypeScript", "SQLite"},
				Facts:        []string{"Languages: TypeScript, Go, CSS", "Last updated 2026-05"},
			},
		},
		Experience:     []Experience{},
		Education:      []Education{{Degree: "BSc Computer Science", Institution: "Example University", End: "2022"}},
		Certifications: []Certification{},
		Inferences:     []string{},
		MissingInfo:    []string{},
	}
}
