package githubintegration

import (
	"errors"
	"testing"

	"github.com/bradleymackey/track-slash/internal/model"
)

func TestParseRepository(t *testing.T) {
	tests := []struct {
		raw         string
		owner, name string
		valid       bool
	}{
		{"acme/widgets", "acme", "widgets", true},
		{"https://github.com/Acme/widgets.git", "Acme", "widgets", true},
		{"", "", "", false},
		{"http://github.com/acme/widgets", "", "", false},
		{"https://github.com/acme/widgets?tab=readme", "", "", false},
		{"https://user@github.com/acme/widgets", "", "", false},
		{"https://example.com/acme/widgets", "", "", false},
		{"acme", "", "", false},
		{"/widgets", "", "", false},
		{"-acme/widgets", "", "", false},
		{"acme/widgets/extra", "", "", false},
	}
	for _, test := range tests {
		owner, name, err := ParseRepository(test.raw)
		if test.valid && (err != nil || owner != test.owner || name != test.name) {
			t.Errorf("ParseRepository(%q) = %q, %q, %v", test.raw, owner, name, err)
		}
		if !test.valid && !errors.Is(err, ErrInvalid) {
			t.Errorf("ParseRepository(%q) error = %v", test.raw, err)
		}
	}
}

func TestParseReference(t *testing.T) {
	connection := model.GitHubConnection{RepositoryOwner: "acme", RepositoryName: "widgets"}
	tests := []struct {
		raw      string
		typeWant model.GitHubResourceType
		branch   string
		pr       int
		valid    bool
	}{
		{"#42", model.GitHubResourcePullRequest, "", 42, true},
		{"pull/42", model.GitHubResourcePullRequest, "", 42, true},
		{"feature/ship-it", model.GitHubResourceBranch, "feature/ship-it", 0, true},
		{"branch/release", model.GitHubResourceBranch, "release", 0, true},
		{"tree/release", model.GitHubResourceBranch, "release", 0, true},
		{"https://github.com/acme/widgets/pull/7", model.GitHubResourcePullRequest, "", 7, true},
		{"https://github.com/acme/widgets/tree/feature/one", model.GitHubResourceBranch, "feature/one", 0, true},
		{"https://github.com/other/widgets/pull/7", "", "", 0, false},
		{"https://github.com/acme/widgets/issues/7", "", "", 0, false},
		{"https://github.com/acme/widgets/pull/7/files", "", "", 0, false},
		{"http://github.com/acme/widgets/pull/7", "", "", 0, false},
		{"#0", "", "", 0, false},
		{"feature..bad", "", "", 0, false},
		{"bad branch", "", "", 0, false},
		{"refs/@{bad", "", "", 0, false},
		{"topic.lock", "", "", 0, false},
		{"@", "", "", 0, false},
		{"/leading", "", "", 0, false},
		{"trailing/", "", "", 0, false},
		{".hidden", "", "", 0, false},
		{"ending.", "", "", 0, false},
		{"double//slash", "", "", 0, false},
		{"control\x00name", "", "", 0, false},
		{"", "", "", 0, false},
	}
	for _, test := range tests {
		got, err := ParseReference(test.raw, connection)
		if test.valid && (err != nil || got.ResourceType != test.typeWant || got.BranchName != test.branch || got.PullRequestNumber != test.pr) {
			t.Errorf("ParseReference(%q) = %+v, %v", test.raw, got, err)
		}
		if !test.valid && !errors.Is(err, ErrInvalid) {
			t.Errorf("ParseReference(%q) error = %v", test.raw, err)
		}
	}
}
