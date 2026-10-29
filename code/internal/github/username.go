package github

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
)

// ErrInvalidUsername is returned when input is not a valid GitHub username or profile URL.
var ErrInvalidUsername = errors.New("enter a valid GitHub username or profile URL (for example octocat or https://github.com/octocat)")

// GitHub usernames: 1-39 alphanumerics or single hyphens, not starting or ending with a hyphen.
var usernameRE = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9]|-[A-Za-z0-9]){0,38}$`)

// NormalizeUsername accepts "octocat", "@octocat", "github.com/octocat" or
// "https://github.com/octocat/" and returns the bare username.
func NormalizeUsername(input string) (string, error) {
	s := strings.TrimSpace(input)
	if s == "" || len(s) > 200 {
		return "", ErrInvalidUsername
	}
	s = strings.TrimPrefix(s, "@")

	lower := strings.ToLower(s)
	if strings.Contains(lower, "github.com") {
		if !strings.Contains(s, "://") {
			s = "https://" + s
		}
		u, err := url.Parse(s)
		if err != nil {
			return "", ErrInvalidUsername
		}
		host := strings.ToLower(u.Hostname())
		if host != "github.com" && host != "www.github.com" {
			return "", ErrInvalidUsername
		}
		if u.Scheme != "https" && u.Scheme != "http" {
			return "", ErrInvalidUsername
		}
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) != 1 || parts[0] == "" {
			return "", ErrInvalidUsername
		}
		s = parts[0]
	}

	if !usernameRE.MatchString(s) {
		return "", ErrInvalidUsername
	}
	return s, nil
}

var repoNameRE = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)

// ValidRepoName reports whether name is a syntactically valid repository name.
func ValidRepoName(name string) bool {
	return repoNameRE.MatchString(name) && name != "." && name != ".."
}
