// Package markdown renders user-written Markdown to sanitized HTML.
package markdown

import (
	"bytes"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer/html"
)

var (
	md = goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		// Raw HTML is escaped by goldmark and anything left is stripped by
		// the sanitizer below; Hard wraps match how people type in chats.
		goldmark.WithRendererOptions(html.WithHardWraps()),
	)
	policy = func() *bluemonday.Policy {
		p := bluemonday.UGCPolicy()
		p.RequireNoReferrerOnLinks(true)
		p.AddTargetBlankToFullyQualifiedLinks(true)
		// GFM task lists.
		p.AllowAttrs("type").Matching(bluemonday.SpaceSeparatedTokens).OnElements("input")
		p.AllowAttrs("checked", "disabled").OnElements("input")
		return p
	}()
)

// ToHTML renders src to HTML that is safe to embed in a page.
func ToHTML(src string) string {
	var buf bytes.Buffer
	if err := md.Convert([]byte(src), &buf); err != nil {
		// goldmark only fails on writer errors, which bytes.Buffer never returns.
		panic(err)
	}
	return policy.Sanitize(buf.String())
}
