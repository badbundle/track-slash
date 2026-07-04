package server

import (
	"bytes"
	"html"
	"html/template"
	"net/url"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"

	"github.com/bradleymackey/track-slash/internal/model"
)

type issueMarkdownTarget struct {
	downloadHref string
	inlineHref   string
	inlineImage  bool
}

type issueMarkdownRenderer struct {
	targets map[string]issueMarkdownTarget
}

func renderIssueDescriptionMarkdown(issue model.Issue, attachments []model.IssueAttachment) template.HTML {
	source := []byte(issue.Description)
	if strings.TrimSpace(issue.Description) == "" {
		return ""
	}
	targets := make(map[string]issueMarkdownTarget, len(attachments))
	for _, attachment := range attachments {
		href := uiIssueAttachmentContentPath(issue, attachment.Object)
		target := issueMarkdownTarget{downloadHref: href}
		if storageObjectSafeInlineImage(attachment.Object) {
			target.inlineHref = href + "?inline=1"
			target.inlineImage = true
		}
		targets[attachment.Object.Ref] = target
	}
	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithRendererOptions(
			renderer.WithNodeRenderers(util.Prioritized(issueMarkdownRenderer{targets: targets}, 900)),
		),
	)
	var out bytes.Buffer
	if err := md.Convert(source, &out); err != nil {
		return template.HTML(html.EscapeString(issue.Description))
	}
	return template.HTML(out.String())
}

func (r issueMarkdownRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindLink, r.renderLink)
	reg.Register(ast.KindImage, r.renderImage)
}

func (r issueMarkdownRenderer) renderLink(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	n := node.(*ast.Link)
	href, ok := r.linkHref(string(n.Destination))
	if !ok {
		return ast.WalkContinue, nil
	}
	if !entering {
		_, _ = w.WriteString("</a>")
		return ast.WalkContinue, nil
	}
	_, _ = w.WriteString(`<a href="`)
	_, _ = w.WriteString(html.EscapeString(href))
	_ = w.WriteByte('"')
	if len(n.Title) > 0 {
		_, _ = w.WriteString(` title="`)
		_, _ = w.WriteString(html.EscapeString(string(n.Title)))
		_ = w.WriteByte('"')
	}
	_ = w.WriteByte('>')
	return ast.WalkContinue, nil
}

func (r issueMarkdownRenderer) renderImage(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	n := node.(*ast.Image)
	alt := markdownPlainText(n, source)
	target, ok := r.objectTarget(string(n.Destination))
	if !ok {
		if src, safe := safeMarkdownImageURL(string(n.Destination)); safe {
			r.renderImageTag(w, src, alt, n.Title)
		} else if href, safe := safeMarkdownURL(string(n.Destination)); safe {
			r.renderFallbackImageLink(w, href, alt, n.Title)
		} else {
			_, _ = w.WriteString(html.EscapeString(alt))
		}
		return ast.WalkSkipChildren, nil
	}
	if !target.inlineImage {
		r.renderFallbackImageLink(w, target.downloadHref, alt, n.Title)
		return ast.WalkSkipChildren, nil
	}
	r.renderImageTag(w, target.inlineHref, alt, n.Title)
	return ast.WalkSkipChildren, nil
}

func (r issueMarkdownRenderer) renderImageTag(w util.BufWriter, src, alt string, title []byte) {
	_, _ = w.WriteString(`<img src="`)
	_, _ = w.WriteString(html.EscapeString(src))
	_, _ = w.WriteString(`" alt="`)
	_, _ = w.WriteString(html.EscapeString(alt))
	_ = w.WriteByte('"')
	if len(title) > 0 {
		_, _ = w.WriteString(` title="`)
		_, _ = w.WriteString(html.EscapeString(string(title)))
		_ = w.WriteByte('"')
	}
	_ = w.WriteByte('>')
}

func (r issueMarkdownRenderer) renderFallbackImageLink(w util.BufWriter, href, alt string, title []byte) {
	label := alt
	if strings.TrimSpace(label) == "" {
		label = href
	}
	_, _ = w.WriteString(`<a href="`)
	_, _ = w.WriteString(html.EscapeString(href))
	_ = w.WriteByte('"')
	if len(title) > 0 {
		_, _ = w.WriteString(` title="`)
		_, _ = w.WriteString(html.EscapeString(string(title)))
		_ = w.WriteByte('"')
	}
	_ = w.WriteByte('>')
	_, _ = w.WriteString(html.EscapeString(label))
	_, _ = w.WriteString("</a>")
}

func (r issueMarkdownRenderer) linkHref(raw string) (string, bool) {
	if target, ok := r.objectTarget(raw); ok {
		return target.downloadHref, true
	}
	return safeMarkdownURL(raw)
}

func (r issueMarkdownRenderer) objectTarget(raw string) (issueMarkdownTarget, bool) {
	ref := strings.TrimSpace(raw)
	if _, err := parseTypedRef(ref, "object"); err != nil {
		return issueMarkdownTarget{}, false
	}
	target, ok := r.targets[ref]
	return target, ok
}

func safeMarkdownURL(raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", false
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return "", false
	}
	if u.Scheme == "" {
		return trimmed, strings.HasPrefix(trimmed, "/") || strings.HasPrefix(trimmed, "#")
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "mailto":
		return trimmed, true
	default:
		return "", false
	}
}

func safeMarkdownImageURL(raw string) (string, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", false
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return "", false
	}
	if u.Scheme == "" {
		return trimmed, strings.HasPrefix(trimmed, "/")
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		return trimmed, true
	default:
		return "", false
	}
}

func markdownPlainText(node ast.Node, source []byte) string {
	var out strings.Builder
	_ = ast.Walk(node, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch current := n.(type) {
		case *ast.Text:
			out.Write(current.Segment.Value(source))
			if current.SoftLineBreak() || current.HardLineBreak() {
				out.WriteByte(' ')
			}
		case *ast.String:
			out.Write(current.Value)
		case *ast.CodeSpan:
			for child := current.FirstChild(); child != nil; child = child.NextSibling() {
				if textNode, ok := child.(*ast.Text); ok {
					out.Write(textNode.Segment.Value(source))
				}
			}
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})
	return strings.TrimSpace(out.String())
}
