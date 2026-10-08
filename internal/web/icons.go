package web

import (
	"fmt"
	"html/template"
	"io/fs"
	"path"
	"strings"
)

// iconSet is the inner markup of each icon in web/icons, by name.
type iconSet map[string]string

// loadIcons reads every <name>.svg in fsys. Only the drawing inside the
// outer <svg> is kept; the icon func supplies its own <svg> element.
func loadIcons(fsys fs.FS) (iconSet, error) {
	files, err := fs.Glob(fsys, "icons/*.svg")
	if err != nil {
		return nil, err
	}
	icons := iconSet{}
	for _, f := range files {
		b, err := fs.ReadFile(fsys, f)
		if err != nil {
			return nil, err
		}
		src := string(b)
		start := strings.Index(src, "<svg")
		if start < 0 {
			return nil, fmt.Errorf("icon %s: no <svg>", f)
		}
		open := strings.IndexByte(src[start:], '>')
		end := strings.LastIndex(src, "</svg>")
		if open < 0 || end < start+open {
			return nil, fmt.Errorf("icon %s: malformed <svg>", f)
		}
		var body strings.Builder
		for line := range strings.Lines(src[start+open+1 : end]) {
			body.WriteString(strings.TrimSpace(line))
		}
		icons[strings.TrimSuffix(path.Base(f), ".svg")] = body.String()
	}
	return icons, nil
}

// html renders an icon as inline SVG so it takes the text colour. Icons are
// decorative: the meaning is in the text next to them or in the button's
// aria-label. An unknown name fails the template, so typos show up in tests.
func (s iconSet) html(name string) (template.HTML, error) {
	body, ok := s[name]
	if !ok {
		return "", fmt.Errorf("unknown icon %q", name)
	}
	return template.HTML(`<svg class="icon" aria-hidden="true" focusable="false" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">` + body + `</svg>`), nil
}
