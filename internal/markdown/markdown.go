// Package markdown renders release notes for display.
//
// Rendered on the Go side rather than in the browser so that raw HTML never
// reaches the page. GitHub's auto-generated notes embed contributor names and
// PR titles — content the launcher does not author — and goldmark escapes raw
// HTML unless explicitly told not to. It is not told not to.
package markdown

import (
	"bytes"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

var renderer = goldmark.New(
	// Autolinks and strikethrough, matching how GitHub renders the same text.
	// Notably absent: the unsafe HTML renderer option.
	goldmark.WithExtensions(extension.GFM),
)

// Render converts Markdown to HTML.
//
// On failure it returns the source as escaped text rather than an error: a
// changelog that renders badly is far better than a launcher that refuses to
// show one.
func Render(source string) string {
	if strings.TrimSpace(source) == "" {
		return ""
	}
	var out bytes.Buffer
	if err := renderer.Convert([]byte(source), &out); err != nil {
		return "<pre>" + escape(source) + "</pre>"
	}
	return out.String()
}

func escape(text string) string {
	replacer := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return replacer.Replace(text)
}
