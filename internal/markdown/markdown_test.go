package markdown

import (
	"strings"
	"testing"
)

func TestToHTML(t *testing.T) {
	out := ToHTML("**bold**\n\n- [x] done\n\n[site](https://example.com)")
	for _, want := range []string{
		"<strong>bold</strong>",
		`<input checked="" disabled="" type="checkbox"`,
		`href="https://example.com"`,
		`rel="nofollow noreferrer noopener"`,
		`target="_blank"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
}

func TestToHTMLStripsDangerousMarkup(t *testing.T) {
	for _, src := range []string{
		"<script>alert(1)</script>",
		"[x](javascript:alert(1))",
		`<img src=x onerror="alert(1)">`,
		`<a href="javascript:alert(1)">x</a>`,
	} {
		out := ToHTML(src)
		for _, bad := range []string{"<script", "javascript:", "onerror"} {
			if strings.Contains(strings.ToLower(out), bad) {
				t.Errorf("%q rendered to %q", src, out)
			}
		}
	}
}
