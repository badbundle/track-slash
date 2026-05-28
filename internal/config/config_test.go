package config

import (
	"reflect"
	"testing"
)

func TestParseList(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"  ", nil},
		{",,,", nil},
		{"https://a.com", []string{"https://a.com"}},
		{"https://a.com,https://b.com", []string{"https://a.com", "https://b.com"}},
		{"  https://a.com  ,  https://b.com  ", []string{"https://a.com", "https://b.com"}},
		{"a,,b", []string{"a", "b"}},
	}
	for _, c := range cases {
		got := parseList(c.in)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("parseList(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestEnvOr(t *testing.T) {
	t.Setenv("X_TEST_DEFINED", "value")
	if got := envOr("X_TEST_DEFINED", "fallback"); got != "value" {
		t.Errorf("envOr defined: got %q, want %q", got, "value")
	}
	if got := envOr("X_TEST_UNDEFINED_KEY_QWERTY", "fallback"); got != "fallback" {
		t.Errorf("envOr undefined: got %q, want %q", got, "fallback")
	}
}
