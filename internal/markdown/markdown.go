// Package markdown renders user-written Markdown to sanitized HTML.
package markdown

import (
	"bytes"
	"sync"

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

// Rendering and sanitizing are the slowest part of drawing a page of
// notes or homework, and pages show the same texts again and again, so the
// results are kept, up to maxCached bytes of Markdown and HTML; when that
// is reached, the cache starts over.
const maxCached = 4 << 20

var cached struct {
	sync.Mutex
	html  map[string]string
	bytes int
}

// ToHTML renders src to HTML that is safe to embed in a page.
func ToHTML(src string) string {
	cached.Lock()
	out, ok := cached.html[src]
	cached.Unlock()
	if ok {
		return out
	}
	out = render(src)
	size := len(src) + len(out)
	cached.Lock()
	defer cached.Unlock()
	if cached.html == nil || cached.bytes+size > maxCached {
		cached.html, cached.bytes = map[string]string{}, 0
	}
	if size <= maxCached {
		if _, dup := cached.html[src]; !dup {
			cached.html[src] = out
			cached.bytes += size
		}
	}
	return out
}

func render(src string) string {
	var buf bytes.Buffer
	if err := md.Convert([]byte(src), &buf); err != nil {
		// goldmark only fails on writer errors, which bytes.Buffer never returns.
		panic(err)
	}
	return policy.Sanitize(buf.String())
}
