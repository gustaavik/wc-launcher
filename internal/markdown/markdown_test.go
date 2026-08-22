package markdown

import (
	"strings"
	"testing"
)

// The real v0.0.1 release body.
const realNotes = `## What's Changed
* feat: auth server and accounts by @gustaavik in https://github.com/gustaavik/wyvencraft/pull/1

## New Contributors
* @gustaavik made their first contribution in https://github.com/gustaavik/wyvencraft/pull/1

**Full Changelog**: https://github.com/gustaavik/wyvencraft/commits/v0.0.1`

func TestRendersARealReleaseBody(t *testing.T) {
	html := Render(realNotes)

	for _, want := range []string{"<h2>What's Changed</h2>", "<li>", "<strong>Full Changelog</strong>"} {
		if !strings.Contains(html, want) {
			t.Errorf("rendered output missing %q:\n%s", want, html)
		}
	}
	// GFM autolinks bare URLs, as GitHub does.
	if !strings.Contains(html, `<a href="https://github.com/gustaavik/wyvencraft/pull/1"`) {
		t.Errorf("URLs should be linked:\n%s", html)
	}
}

// Release notes contain PR titles written by other people. Raw HTML in one must
// not reach the page, because the launcher renders this with innerHTML.
func TestRawHTMLInNotesIsEscapedNotEmitted(t *testing.T) {
	html := Render("* fix: <img src=x onerror=alert(1)> by @someone\n\n<script>alert(2)</script>")

	if strings.Contains(html, "<script>") {
		t.Errorf("a script tag survived:\n%s", html)
	}
	if strings.Contains(html, "<img") {
		t.Errorf("an img tag survived:\n%s", html)
	}
	// goldmark's default is to drop raw HTML entirely rather than escape it,
	// which is stricter still — the tag becomes a comment, not markup.
	if !strings.Contains(html, "raw HTML omitted") {
		t.Errorf("the raw HTML should have been dropped:\n%s", html)
	}
	// The surrounding prose survives, so the note is still readable.
	if !strings.Contains(html, "by @someone") {
		t.Errorf("the text around it should remain:\n%s", html)
	}
}

// A javascript: URL in a link is the other way to get script execution.
func TestAJavascriptURLIsNotLinked(t *testing.T) {
	html := Render("[click me](javascript:alert(1))")

	if strings.Contains(html, `href="javascript:`) {
		t.Errorf("a javascript: link survived:\n%s", html)
	}
}

func TestEmptyNotesRenderToNothing(t *testing.T) {
	if got := Render(""); got != "" {
		t.Errorf("Render(\"\") = %q", got)
	}
	if got := Render("   \n  "); got != "" {
		t.Errorf("whitespace-only notes should render to nothing, got %q", got)
	}
}
