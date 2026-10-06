package store

import "context"

// LessonType is the kind of class a link is for.
type LessonType string

const (
	LessonLecture  LessonType = "lecture"
	LessonPractice LessonType = "practice"
	LessonLab      LessonType = "lab"
)

// ClassLink is a row of the class_links table: the meeting link for a
// subject's lectures, practices or labs.
type ClassLink struct {
	ID         int64
	GroupID    int64
	SubjectID  int64
	LessonType LessonType
	URL        string
	Note       string
	// SubjectName is read from subjects; writes ignore it.
	SubjectName string
}

// classLinkSelect reads class links with their subject's name; add a WHERE on l.
const classLinkSelect = `SELECT l.id, l.group_id, l.subject_id, l.lesson_type, l.url, l.note, s.name
	FROM class_links l JOIN subjects s ON s.id = l.subject_id `

func scanClassLink(row interface{ Scan(...any) error }) (*ClassLink, error) {
	var l ClassLink
	if err := row.Scan(&l.ID, &l.GroupID, &l.SubjectID, &l.LessonType, &l.URL, &l.Note, &l.SubjectName); err != nil {
		return nil, mapErr(err)
	}
	return &l, nil
}

// CreateClassLink inserts a class link and sets l.ID.
func (q *Queries) CreateClassLink(ctx context.Context, l *ClassLink) error {
	res, err := q.db.ExecContext(ctx,
		`INSERT INTO class_links (group_id, subject_id, lesson_type, url, note) VALUES (?, ?, ?, ?, ?)`,
		l.GroupID, l.SubjectID, l.LessonType, l.URL, l.Note)
	if err != nil {
		return mapErr(err)
	}
	l.ID, err = res.LastInsertId()
	return err
}

// UpdateClassLink saves a class link.
func (q *Queries) UpdateClassLink(ctx context.Context, l *ClassLink) error {
	return expectOne(q.db.ExecContext(ctx,
		`UPDATE class_links SET subject_id = ?, lesson_type = ?, url = ?, note = ? WHERE id = ? AND group_id = ?`,
		l.SubjectID, l.LessonType, l.URL, l.Note, l.ID, l.GroupID))
}

// DeleteClassLink removes a class link.
func (q *Queries) DeleteClassLink(ctx context.Context, groupID, id int64) error {
	return expectOne(q.db.ExecContext(ctx, `DELETE FROM class_links WHERE id = ? AND group_id = ?`, id, groupID))
}

// ClassLinkByID returns a class link of the group.
func (q *Queries) ClassLinkByID(ctx context.Context, groupID, id int64) (*ClassLink, error) {
	return scanClassLink(q.db.QueryRowContext(ctx,
		classLinkSelect+`WHERE l.id = ? AND l.group_id = ?`, id, groupID))
}

// ListClassLinks returns the group's class links ordered by subject name,
// then lecture, practice, lab.
func (q *Queries) ListClassLinks(ctx context.Context, groupID int64) ([]*ClassLink, error) {
	return queryAll(ctx, q, scanClassLink,
		classLinkSelect+`WHERE l.group_id = ?
		 ORDER BY s.name COLLATE NOCASE,
		          CASE l.lesson_type WHEN 'lecture' THEN 0 WHEN 'practice' THEN 1 ELSE 2 END,
		          l.id`, groupID)
}
