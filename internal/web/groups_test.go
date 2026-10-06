package web

import (
	"slices"
	"testing"

	"github.com/sqprrr/ExtendedTimetable/internal/service"
	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

func TestParseLinks(t *testing.T) {
	got := parseLinks("Manual — https://a.example\r\n\n  https://b.example  \nVariant 3: https://c.example\nLab | https://d.example\n")
	want := []service.LinkInput{
		{Title: "Manual", URL: "https://a.example"},
		{URL: "https://b.example"},
		{Title: "Variant 3:", URL: "https://c.example"},
		{Title: "Lab", URL: "https://d.example"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

// Saving the edit form without touching the links must not change them.
func TestLinksRoundTrip(t *testing.T) {
	links := []*store.HomeworkLink{
		{Title: "Variant 3:", URL: "https://a.example"},
		{Title: "ends with —", URL: "https://b.example"},
		{Title: "-", URL: "https://c.example"},
		{Title: "two  spaces | inside", URL: "https://d.example"},
		{URL: "https://e.example"},
	}
	got := parseLinks(formatLinks(links))
	if len(got) != len(links) {
		t.Fatalf("got %d links, want %d: %+v", len(got), len(links), got)
	}
	for i, l := range links {
		if got[i].Title != l.Title || got[i].URL != l.URL {
			t.Errorf("link %d: got %+v, want %q %q", i, got[i], l.Title, l.URL)
		}
	}
}
