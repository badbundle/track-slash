package store

import "testing"

func TestProjectMemberCandidateQueryReady(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		query string
		want  bool
	}{
		{query: "", want: false},
		{query: "  ", want: false},
		{query: "a", want: false},
		{query: "å", want: false},
		{query: "ab", want: true},
		{query: " åb ", want: true},
	} {
		if got := ProjectMemberCandidateQueryReady(test.query); got != test.want {
			t.Errorf("ProjectMemberCandidateQueryReady(%q) = %v, want %v", test.query, got, test.want)
		}
	}
}
