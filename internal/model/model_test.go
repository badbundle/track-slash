package model

import "testing"

func TestStatusValid(t *testing.T) {
	cases := []struct {
		in   Status
		want bool
	}{
		{StatusTodo, true},
		{StatusInProgress, true},
		{StatusDone, true},
		{"", false},
		{"open", false},
		{"DONE", false},
		{"in progress", false},
	}
	for _, c := range cases {
		t.Run(string(c.in), func(t *testing.T) {
			if got := c.in.Valid(); got != c.want {
				t.Fatalf("Status(%q).Valid() = %v, want %v", c.in, got, c.want)
			}
		})
	}
}
