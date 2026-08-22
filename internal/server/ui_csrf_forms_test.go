package server

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

var (
	uiFormOpenPattern  = regexp.MustCompile(`(?is)<form\b[^>]*>`)
	uiFormClosePattern = regexp.MustCompile(`(?i)</form>`)
	uiPostFormPattern  = regexp.MustCompile(`(?i)method\s*=\s*"post"`)
	uiCSRFFieldPattern = regexp.MustCompile(`(?i)\{\{template "csrf-field"|name="csrf_token"`)
)

// Every form that posts must carry the token in its own markup. Relying on
// app.js to inject it at submit time loses the race against a user who types
// into an autofocused field and presses Enter before the script has run.
func TestEveryPostFormRendersACSRFField(t *testing.T) {
	t.Parallel()

	paths, err := fs.Glob(uiTemplateFS, "templates/*.html")
	if err != nil {
		t.Fatalf("glob templates: %v", err)
	}
	checked := 0
	for _, path := range paths {
		body, err := uiTemplateFS.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		source := string(body)
		for _, open := range uiFormOpenPattern.FindAllStringIndex(source, -1) {
			tag := source[open[0]:open[1]]
			if !uiPostFormPattern.MatchString(tag) {
				continue
			}
			checked++
			rest := source[open[1]:]
			end := uiFormClosePattern.FindStringIndex(rest)
			if end == nil {
				t.Fatalf("%s: unclosed form %s", path, tag)
			}
			if !uiCSRFFieldPattern.MatchString(rest[:end[0]]) {
				t.Errorf("%s: form has no csrf_token field: %s", path, strings.TrimSpace(tag))
			}
		}
	}
	if checked == 0 {
		t.Fatal("found no posting forms to check")
	}
	t.Logf("checked %d posting forms", checked)
}
