package github

import "time"

// User is the subset of the GitHub user object the app uses.
type User struct {
	Login       string    `json:"login"`
	Name        string    `json:"name"`
	AvatarURL   string    `json:"avatar_url"`
	HTMLURL     string    `json:"html_url"`
	Bio         string    `json:"bio"`
	Company     string    `json:"company"`
	Blog        string    `json:"blog"`
	Location    string    `json:"location"`
	Email       string    `json:"email"`
	Twitter     string    `json:"twitter_username"`
	PublicRepos int       `json:"public_repos"`
	Followers   int       `json:"followers"`
	CreatedAt   time.Time `json:"created_at"`
}

// Repo is the subset of the GitHub repository object the app uses.
type Repo struct {
	Name        string    `json:"name"`
	FullName    string    `json:"full_name"`
	HTMLURL     string    `json:"html_url"`
	Description string    `json:"description"`
	Homepage    string    `json:"homepage"`
	Language    string    `json:"language"`
	Topics      []string  `json:"topics"`
	Stars       int       `json:"stargazers_count"`
	Forks       int       `json:"forks_count"`
	Fork        bool      `json:"fork"`
	Archived    bool      `json:"archived"`
	Size        int       `json:"size"` // KiB
	License     *License  `json:"license"`
	CreatedAt   time.Time `json:"created_at"`
	PushedAt    time.Time `json:"pushed_at"`
}

// License is the repository license summary.
type License struct {
	SPDXID string `json:"spdx_id"`
	Name   string `json:"name"`
}
