package web

import (
	"time"

	"github.com/sqprrr/ExtendedTimetable/internal/service"
	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

// hwList is the homework page's list, grouped by when things are due
// (service.GroupHomework).
type hwList struct {
	service.HomeworkGroups
	// OpenDone shows the Done group, collapsed unless the done filter is on.
	OpenDone bool
}

// groupHomework groups a homework list for a page filtered by f; weeks are
// those of loc.
func groupHomework(loc *time.Location, now time.Time, list []*service.Homework, f service.HomeworkFilter) hwList {
	return hwList{HomeworkGroups: service.GroupHomework(list, now, loc), OpenDone: f.Status == store.StatusDone}
}
