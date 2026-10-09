package web

import (
	"net/url"
	"strconv"

	"github.com/sqprrr/ExtendedTimetable/internal/service"
	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

// listFilter narrows the class links (by subject and lesson type) and the
// recordings (by subject and kind), from ?subject_id=…&lesson_type=…&kind=….
// Values that are not an id, a lesson type or a kind are ignored, so a stale
// or edited link shows the whole list.
type listFilter struct {
	SubjectID  int64
	LessonType store.LessonType
	Kind       store.ResourceKind
	// Subjects are the chips: the subjects that have something in the
	// unfiltered list, in the order of the group's subjects.
	Subjects []*store.Subject
}

func parseListFilter(q url.Values) listFilter {
	var f listFilter
	if id, err := strconv.ParseInt(q.Get("subject_id"), 10, 64); err == nil && id > 0 {
		f.SubjectID = id
	}
	for _, lt := range service.LessonTypes {
		if q.Get("lesson_type") == string(lt) {
			f.LessonType = lt
		}
	}
	for _, k := range []store.ResourceKind{store.ResourceRecording, store.ResourceSolution} {
		if q.Get("kind") == string(k) {
			f.Kind = k
		}
	}
	return f
}

// Active reports whether the filter leaves anything out.
func (f listFilter) Active() bool {
	return f.SubjectID != 0 || f.LessonType != "" || f.Kind != ""
}

// subjectsWith keeps the subjects that have an item, by the items' subject ids.
func subjectsWith(subjects []*store.Subject, ids map[int64]bool) []*store.Subject {
	var out []*store.Subject
	for _, s := range subjects {
		if ids[s.ID] {
			out = append(out, s)
		}
	}
	return out
}

// filterClassLinks returns the links that pass f and sets f's subject chips.
func filterClassLinks(f *listFilter, subjects []*store.Subject, links []*store.ClassLink) []*store.ClassLink {
	ids := map[int64]bool{}
	var out []*store.ClassLink
	for _, l := range links {
		ids[l.SubjectID] = true
		if (f.SubjectID == 0 || l.SubjectID == f.SubjectID) && (f.LessonType == "" || l.LessonType == f.LessonType) {
			out = append(out, l)
		}
	}
	f.Subjects = subjectsWith(subjects, ids)
	return out
}

// filterResources returns the recordings and solutions that pass f and sets
// f's subject chips.
func filterResources(f *listFilter, subjects []*store.Subject, rs []*store.ResourceLink) []*store.ResourceLink {
	ids := map[int64]bool{}
	var out []*store.ResourceLink
	for _, r := range rs {
		ids[r.SubjectID] = true
		if (f.SubjectID == 0 || r.SubjectID == f.SubjectID) && (f.Kind == "" || r.Kind == f.Kind) {
			out = append(out, r)
		}
	}
	f.Subjects = subjectsWith(subjects, ids)
	return out
}
