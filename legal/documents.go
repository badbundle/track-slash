// Package legal embeds the canonical documents published by the application.
package legal

import _ "embed"

const PreviewTermsVersion = "2026-07-18"

type Document struct {
	Slug     string
	Title    string
	Version  string
	Markdown string
}

var (
	//go:embed TERMS.md
	termsMarkdown string

	//go:embed PRIVACY.md
	privacyMarkdown string

	//go:embed SECURITY.md
	securityMarkdown string

	Terms = Document{
		Slug:     "terms",
		Title:    "Preview Terms",
		Version:  PreviewTermsVersion,
		Markdown: termsMarkdown,
	}
	Privacy = Document{
		Slug:     "privacy",
		Title:    "Privacy Notice",
		Version:  "2026-07-18",
		Markdown: privacyMarkdown,
	}
	Security = Document{
		Slug:     "security",
		Title:    "Security Policy",
		Version:  "2026-07-18",
		Markdown: securityMarkdown,
	}
)
