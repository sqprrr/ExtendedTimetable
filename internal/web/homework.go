package web

import (
	"time"

	"github.com/sqprrr/ExtendedTimetable/internal/service"
	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

// hwGroup is one group of the homework list: what is overdue, due this
// week, due later, without a deadline, and done.
type hwGroup struct {
	// Key names the group in the locale files (hw.group.<key>).
	Key   string
	Items []*service.Homework
}

// hwList is the homework page's list, grouped by when things are due.
type hwList struct {
	Groups []hwGroup
	// Done is last and collapsed unless the done filter is on.
	Done     []*service.Homework
	OpenDone bool
	// Overdue and ThisWeek count the page subtitle's figures.
	Overdue, ThisWeek int
}

// groupHomework sorts a homework list into its groups. The list keeps its
// order (by deadline) inside each group; empty groups are left out.
func (h *Handler) groupHomework(now time.Time, list []*service.Homework, f service.HomeworkFilter) hwList {
	weekEnd := weekStart("", now, h.loc).AddDate(0, 0, 7)
	var overdue, week, later, none []*service.Homework
	out := hwList{OpenDone: f.Status == store.StatusDone}
	for _, hw := range list {
		switch {
		case hw.Tracked && hw.Status == store.StatusDone:
			out.Done = append(out.Done, hw)
		case hw.Overdue:
			overdue = append(overdue, hw)
		case hw.DueAt == nil:
			none = append(none, hw)
		case hw.DueAt.Before(weekEnd):
			week = append(week, hw)
		default:
			later = append(later, hw)
		}
	}
	for _, g := range []hwGroup{{"overdue", overdue}, {"week", week}, {"later", later}, {"none", none}} {
		if len(g.Items) > 0 {
			out.Groups = append(out.Groups, g)
		}
	}
	out.Overdue, out.ThisWeek = len(overdue), len(week)
	return out
}
