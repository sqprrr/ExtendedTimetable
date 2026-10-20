package web

import (
	"encoding/xml"
	"fmt"
	"html/template"
	"io/fs"
	"strings"
)

// iconSet is the inner markup of each icon in the sprite, by name.
type iconSet map[string]string

// sprite is web/icons/sprite.svg: one <symbol> per icon.
type sprite struct {
	Symbols []struct {
		ID      string `xml:"id,attr"`
		ViewBox string `xml:"viewBox,attr"`
		Body    string `xml:",innerxml"`
	} `xml:"symbol"`
}

// loadIcons reads the symbols of icons/sprite.svg in fsys. Only the drawing
// inside each <symbol> is kept; the icon func supplies its own <svg>
// element, so every symbol must be on the 24px grid it draws.
func loadIcons(fsys fs.FS) (iconSet, error) {
	b, err := fs.ReadFile(fsys, "icons/sprite.svg")
	if err != nil {
		return nil, err
	}
	var sp sprite
	if err := xml.Unmarshal(b, &sp); err != nil {
		return nil, fmt.Errorf("icon sprite: %w", err)
	}
	icons := iconSet{}
	for _, s := range sp.Symbols {
		switch {
		case s.ID == "":
			return nil, fmt.Errorf("icon sprite: a symbol has no id")
		case s.ViewBox != "0 0 24 24":
			return nil, fmt.Errorf("icon %s: viewBox %q, want 0 0 24 24", s.ID, s.ViewBox)
		case icons[s.ID] != "":
			return nil, fmt.Errorf("icon %s: defined twice", s.ID)
		}
		var body strings.Builder
		for line := range strings.Lines(s.Body) {
			body.WriteString(strings.TrimSpace(line))
		}
		icons[s.ID] = body.String()
	}
	if len(icons) == 0 {
		return nil, fmt.Errorf("icon sprite: no symbols")
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
