package githubintegration

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"unicode"

	"github.com/bradleymackey/track-slash/internal/model"
)

type Reference struct {
	ResourceType      model.GitHubResourceType
	BranchName        string
	PullRequestNumber int
}

func ParseRepository(raw string) (string, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", fmt.Errorf("repository is required: %w", ErrInvalid)
	}
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil || !strings.EqualFold(u.Scheme, "https") || !strings.EqualFold(u.Hostname(), "github.com") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return "", "", fmt.Errorf("repository must be a github.com URL: %w", ErrInvalid)
		}
		raw = strings.Trim(u.Path, "/")
	}
	parts := strings.Split(raw, "/")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("repository must be owner/name: %w", ErrInvalid)
	}
	owner := strings.TrimSpace(parts[0])
	name := strings.TrimSuffix(strings.TrimSpace(parts[1]), ".git")
	if !validGitHubName(owner) || !validGitHubName(name) {
		return "", "", fmt.Errorf("repository owner or name is invalid: %w", ErrInvalid)
	}
	return owner, name, nil
}

func ParseReference(raw string, connection model.GitHubConnection) (Reference, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Reference{}, fmt.Errorf("branch or pull request is required: %w", ErrInvalid)
	}
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil || !strings.EqualFold(u.Scheme, "https") || !strings.EqualFold(u.Hostname(), "github.com") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return Reference{}, fmt.Errorf("reference must be a github.com URL: %w", ErrInvalid)
		}
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) < 4 || !strings.EqualFold(parts[0], connection.RepositoryOwner) || !strings.EqualFold(strings.TrimSuffix(parts[1], ".git"), connection.RepositoryName) {
			return Reference{}, fmt.Errorf("reference belongs to another repository: %w", ErrInvalid)
		}
		switch parts[2] {
		case "pull":
			if len(parts) != 4 {
				return Reference{}, fmt.Errorf("pull request URL is invalid: %w", ErrInvalid)
			}
			return parsePullNumber(parts[3])
		case "tree":
			if len(parts) < 4 {
				return Reference{}, fmt.Errorf("branch URL is invalid: %w", ErrInvalid)
			}
			return parseBranch(strings.Join(parts[3:], "/"))
		default:
			return Reference{}, fmt.Errorf("reference must identify a branch or pull request: %w", ErrInvalid)
		}
	}

	switch {
	case strings.HasPrefix(raw, "#"):
		return parsePullNumber(strings.TrimPrefix(raw, "#"))
	case strings.HasPrefix(raw, "pull/"):
		return parsePullNumber(strings.TrimPrefix(raw, "pull/"))
	case strings.HasPrefix(raw, "branch/"):
		return parseBranch(strings.TrimPrefix(raw, "branch/"))
	case strings.HasPrefix(raw, "tree/"):
		return parseBranch(strings.TrimPrefix(raw, "tree/"))
	default:
		return parseBranch(raw)
	}
}

func parsePullNumber(raw string) (Reference, error) {
	number, err := strconv.Atoi(raw)
	if err != nil || number <= 0 {
		return Reference{}, fmt.Errorf("pull request number is invalid: %w", ErrInvalid)
	}
	return Reference{ResourceType: model.GitHubResourcePullRequest, PullRequestNumber: number}, nil
}

func parseBranch(raw string) (Reference, error) {
	if !validBranch(raw) {
		return Reference{}, fmt.Errorf("branch name is invalid: %w", ErrInvalid)
	}
	return Reference{ResourceType: model.GitHubResourceBranch, BranchName: raw}, nil
}

func validGitHubName(raw string) bool {
	if len(raw) == 0 || len(raw) > 100 || raw[0] == '-' || raw[len(raw)-1] == '-' {
		return false
	}
	for _, r := range raw {
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return true
}

func validBranch(raw string) bool {
	if len(raw) == 0 || len(raw) > 255 || raw == "@" || strings.HasPrefix(raw, "/") || strings.HasSuffix(raw, "/") || strings.HasPrefix(raw, ".") || strings.HasSuffix(raw, ".") {
		return false
	}
	if strings.Contains(raw, "..") || strings.Contains(raw, "@{") || strings.Contains(raw, "//") || strings.ContainsAny(raw, " ~^:?*[\\") {
		return false
	}
	for _, part := range strings.Split(raw, "/") {
		if part == "" || strings.HasSuffix(part, ".lock") {
			return false
		}
	}
	for _, r := range raw {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
