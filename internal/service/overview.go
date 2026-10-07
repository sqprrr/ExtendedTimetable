package service

import (
	"context"
	"time"

	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

const (
	// overviewHomework is how many assignments the overview shows.
	overviewHomework = 6
	// overviewOverdue is how long an overdue assignment stays on the overview.
	overviewOverdue = 7 * 24 * time.Hour
	// overviewNotes is how many unpinned notes the overview shows, after
	// every pinned one.
	overviewNotes = 3
)

// Overview is the front page of a group.
type Overview struct {
	// Homework is what is due next, starting with assignments that became
	// overdue in the last week.
	Homework   []*Homework
	Notes      []*store.Note
	ClassLinks []*store.ClassLink
	// Today is today's classes and the class in progress or next.
	Today *Schedule
}

// GroupOverview returns the front page of a group.
func (s *Service) GroupOverview(ctx context.Context, groupID int64) (*Overview, error) {
	v, err := canView(ctx, groupID)
	if err != nil {
		return nil, err
	}
	hws, err := s.store.UpcomingHomework(ctx, groupID, s.now().Add(-overviewOverdue), overviewHomework)
	if err != nil {
		return nil, err
	}
	notes, err := s.store.RecentNotes(ctx, groupID, overviewNotes)
	if err != nil {
		return nil, err
	}
	links, err := s.store.ListClassLinks(ctx, groupID)
	if err != nil {
		return nil, err
	}
	views, err := s.homeworkViews(ctx, v, groupID, hws)
	if err != nil {
		return nil, err
	}
	g, err := s.store.GroupByID(ctx, groupID)
	if err != nil {
		return nil, notFound(err)
	}
	today, err := s.todaySchedule(ctx, g, links)
	if err != nil {
		return nil, err
	}
	return &Overview{Homework: views, Notes: notes, ClassLinks: links, Today: today}, nil
}
