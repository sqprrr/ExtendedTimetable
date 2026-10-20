package service

import (
	"context"
	"time"

	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

// HomeworkGroup is one group of a homework list. Key names it in the
// locale files (hw.group.<key>): overdue, week, later or none.
type HomeworkGroup struct {
	Key   string
	Items []*Homework
}

// HomeworkGroups is a homework list sorted by when things are due: what is
// overdue, due this week, due later and without a deadline, then what the
// viewer has done.
type HomeworkGroups struct {
	// Groups are the open groups in that order, empty ones left out.
	Groups []HomeworkGroup
	Done   []*Homework
	// Overdue and ThisWeek count those two groups.
	Overdue, ThisWeek int
}

// GroupHomework sorts a homework list into its groups; this week is the
// week of now in loc, from Monday. The list keeps its order (by deadline)
// inside each group.
func GroupHomework(list []*Homework, now time.Time, loc *time.Location) HomeworkGroups {
	weekEnd := StartOfWeek(now, loc).AddDate(0, 0, 7)
	var out HomeworkGroups
	var overdue, week, later, none []*Homework
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
	for _, g := range []HomeworkGroup{{"overdue", overdue}, {"week", week}, {"later", later}, {"none", none}} {
		if len(g.Items) > 0 {
			out.Groups = append(out.Groups, g)
		}
	}
	out.Overdue, out.ThisWeek = len(overdue), len(week)
	return out
}

// StartOfWeek returns midnight of the Monday of t's week in loc.
func StartOfWeek(t time.Time, loc *time.Location) time.Time {
	day := StartOfDay(t, loc)
	return day.AddDate(0, 0, -(int(day.Weekday())+6)%7)
}

// HomeworkDue counts the group's assignments the viewer still has to hand
// in by the end of this week, overdue ones included: the Overdue and This
// week groups of GroupHomework, without reading the assignments. It is 0
// for those who keep no tracker in the group.
func (s *Service) HomeworkDue(ctx context.Context, groupID int64) (int, error) {
	v, err := canView(ctx, groupID)
	if err != nil {
		return 0, err
	}
	if !v.CanTrackGroup(groupID) {
		return 0, nil
	}
	weekEnd := StartOfWeek(s.now(), s.Location(ctx)).AddDate(0, 0, 7)
	return s.store.CountHomeworkDue(ctx, groupID, v.UserID, weekEnd)
}
