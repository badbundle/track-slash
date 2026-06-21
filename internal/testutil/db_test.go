package testutil

import (
	"strings"
	"testing"
)

func TestDatabaseNameFromURL(t *testing.T) {
	t.Parallel()

	got, err := databaseNameFromURL("postgres://track:track@localhost:5436/track_test?sslmode=disable")
	if err != nil {
		t.Fatalf("databaseNameFromURL: %v", err)
	}
	if got != "track_test" {
		t.Fatalf("database name = %q, want track_test", got)
	}
}

func TestDatabaseURLWithName(t *testing.T) {
	t.Parallel()

	got, err := databaseURLWithName("postgres://track:track@localhost:5436/track_test?sslmode=disable", "track_test_clone")
	if err != nil {
		t.Fatalf("databaseURLWithName: %v", err)
	}
	want := "postgres://track:track@localhost:5436/track_test_clone?sslmode=disable"
	if got != want {
		t.Fatalf("URL = %q, want %q", got, want)
	}
}

func TestDatabaseURLRejectsUnsupportedScheme(t *testing.T) {
	t.Parallel()

	if _, err := databaseNameFromURL("mysql://track:track@localhost/track_test"); err == nil {
		t.Fatal("databaseNameFromURL err = nil, want unsupported scheme error")
	}
	if _, err := databaseURLWithName("mysql://track:track@localhost/track_test", "clone"); err == nil {
		t.Fatal("databaseURLWithName err = nil, want unsupported scheme error")
	}
}

func TestSanitizeDatabaseNamePrefix(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{name: "keeps safe characters", in: "track_test09", want: "track_test09"},
		{name: "normalizes punctuation", in: "Track-Test!!DB", want: "track_test_db"},
		{name: "starts with letter", in: "123", want: "db_123"},
		{name: "empty fallback", in: "!!!", want: "db"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := sanitizeDatabaseNamePrefix(tc.in); got != tc.want {
				t.Fatalf("sanitizeDatabaseNamePrefix(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestBuildDatabaseNameLength(t *testing.T) {
	t.Parallel()

	got := buildDatabaseName(strings.Repeat("a", 80), "template", "12345", "abcdef")
	if len(got) > maxPostgresIdentifierLen {
		t.Fatalf("database name length = %d, want <= %d", len(got), maxPostgresIdentifierLen)
	}
	if !strings.Contains(got, "_template_12345_abcdef") {
		t.Fatalf("database name = %q, want suffix preserved", got)
	}
}

func TestQuoteIdent(t *testing.T) {
	t.Parallel()

	if got := quoteIdent(`track"test`); got != `"track""test"` {
		t.Fatalf("quoteIdent = %q", got)
	}
}
