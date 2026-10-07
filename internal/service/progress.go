package service

import (
	"context"
	"errors"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

// Each member keeps a private tracker of their homework: a status and the
// grade they got. Every method here works on the viewer's own rows only, so
// nobody, leaders and superadmins included, can read anyone else's.

// Statuses lists the homework statuses in order.
var Statuses = []store.ProgressStatus{store.StatusNotStarted, store.StatusInProgress, store.StatusDone}

// NextStatus is the status a one-click toggle moves to: not started → in
// progress → done → not started.
func NextStatus(st store.ProgressStatus) store.ProgressStatus {
	for i, s := range Statuses {
		if s == st {
			return Statuses[(i+1)%len(Statuses)]
		}
	}
	return store.StatusNotStarted
}

// canTrack checks that the viewer has a tracker in the group.
func canTrack(ctx context.Context, groupID int64) (*Viewer, error) {
	v, err := requireViewer(ctx)
	if err != nil {
		return nil, err
	}
	if !v.CanTrackGroup(groupID) {
		return nil, ErrForbidden
	}
	return v, nil
}

// ProgressInput changes the viewer's progress on an assignment.
type ProgressInput struct {
	// Status is the new status; nil keeps the current one.
	Status *store.ProgressStatus
	// SetGrade replaces the grade with Grade (nil clears it); false keeps it.
	SetGrade bool
	Grade    *float64
}

// UpdateProgress changes the viewer's own status and grade for an assignment
// and returns the assignment as the viewer now sees it.
func (s *Service) UpdateProgress(ctx context.Context, groupID, homeworkID int64, in ProgressInput) (*Homework, error) {
	v, err := canTrack(ctx, groupID)
	if err != nil {
		return nil, err
	}
	if in.Status != nil && !validStatus(*in.Status) {
		return nil, inputError("status", "err.status")
	}
	var out *Homework
	err = s.store.InTx(ctx, func(q *store.Queries) error {
		hw, err := q.HomeworkByID(ctx, groupID, homeworkID)
		if err != nil {
			return notFound(err)
		}
		p, err := q.ProgressFor(ctx, v.UserID, homeworkID)
		if errors.Is(err, store.ErrNotFound) {
			p = &store.Progress{UserID: v.UserID, HomeworkID: homeworkID, Status: store.StatusNotStarted}
		} else if err != nil {
			return err
		}
		if in.Status != nil {
			p.Status = *in.Status
		}
		if in.SetGrade {
			if p.Grade, err = grade(in.Grade, hw.MaxPoints); err != nil {
				return err
			}
		}
		p.UpdatedAt = s.now()
		if err := q.UpsertProgress(ctx, p); err != nil {
			return err
		}
		out = homeworkView(hw, true, p, s.now())
		out.Links, err = q.HomeworkLinks(ctx, homeworkID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func validStatus(st store.ProgressStatus) bool {
	for _, s := range Statuses {
		if s == st {
			return true
		}
	}
	return false
}

// grade checks an optional grade against the assignment's max points.
func grade(g, max *float64) (*float64, error) {
	if g == nil {
		return nil, nil
	}
	if math.IsNaN(*g) || math.IsInf(*g, 0) || *g < 0 {
		return nil, inputError("grade", "err.grade_negative")
	}
	if max != nil && *g > *max {
		return nil, inputError("grade", "err.grade_over_max", "Max", strconv.FormatFloat(*max, 'f', -1, 64))
	}
	if *g > maxPoints {
		return nil, inputError("grade", "err.grade_too_big", "Max", maxPoints)
	}
	v := *g
	return &v, nil
}

// Totals sums grades over the assignments that have max points; the others
// cannot be put on the same scale and are left out.
type Totals struct {
	// Assignments counts the assignments with max points; Graded those of
	// them the viewer has a grade for.
	Assignments int
	Graded      int
	// Earned is the sum of the viewer's grades, each capped at its max
	// points; Max the sum of max points, graded or not.
	Earned float64
	Max    float64
}

// add counts an assignment if it has max points. A grade above them (the
// leader lowered max points after it was saved) counts as the max.
func (t *Totals) add(hw *Homework) {
	if hw.MaxPoints == nil {
		return
	}
	t.Assignments++
	t.Max += *hw.MaxPoints
	if hw.Grade != nil {
		t.Graded++
		t.Earned += min(*hw.Grade, *hw.MaxPoints)
	}
}

// SubjectTotals are the totals of one subject.
type SubjectTotals struct {
	SubjectID   int64
	SubjectName string
	Totals
}

// Grades is the viewer's "my grades" page: totals per subject and overall.
type Grades struct {
	Subjects []*SubjectTotals
	Overall  Totals
}

// MyGrades sums the viewer's own grades in a group per subject (sum of
// grades / sum of max points) and overall.
func (s *Service) MyGrades(ctx context.Context, groupID int64) (*Grades, error) {
	v, err := canTrack(ctx, groupID)
	if err != nil {
		return nil, err
	}
	hws, err := s.store.ListHomework(ctx, groupID)
	if err != nil {
		return nil, err
	}
	views, err := s.homeworkViews(ctx, v, groupID, hws)
	if err != nil {
		return nil, err
	}
	g := &Grades{}
	bySubject := map[int64]*SubjectTotals{}
	for _, hw := range views {
		if hw.MaxPoints == nil {
			continue
		}
		st := bySubject[hw.SubjectID]
		if st == nil {
			st = &SubjectTotals{SubjectID: hw.SubjectID, SubjectName: hw.SubjectName}
			bySubject[hw.SubjectID] = st
			g.Subjects = append(g.Subjects, st)
		}
		st.add(hw)
		g.Overall.add(hw)
	}
	sort.Slice(g.Subjects, func(i, j int) bool {
		return strings.ToLower(g.Subjects[i].SubjectName) < strings.ToLower(g.Subjects[j].SubjectName)
	})
	return g, nil
}
