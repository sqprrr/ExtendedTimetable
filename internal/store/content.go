package store

import (
	"context"
	"time"
)

// Subject is a course the group studies.
type Subject struct {
	ID        int64
	GroupID   int64
	Name      string
	ShortName string
}

// ClassLink is a meeting link for one subject and lesson type.
type ClassLink struct {
	ID          int64
	GroupID     int64
	SubjectID   int64
	SubjectName string
	LessonType  string
	URL         string
	Note        string
}

// Homework is an assignment; the grade-tracking fields live elsewhere.
type Homework struct {
	ID            int64
	GroupID       int64
	SubjectID     int64
	SubjectName   string
	Title         string
	DescriptionMD string
	DueAt         time.Time
	MaxPoints     *float64
	CreatedBy     *int64
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// HomeworkLink is a link attached to an assignment.
type HomeworkLink struct {
	ID         int64
	HomeworkID int64
	Title      string
	URL        string
}

// Note is a leader's post.
type Note struct {
	ID        int64
	GroupID   int64
	Title     string
	BodyMD    string
	Pinned    bool
	CreatedBy *int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ResourceLink is a recording or a solution, attached to a subject.
type ResourceLink struct {
	ID          int64
	GroupID     int64
	SubjectID   int64
	SubjectName string
	Kind        string
	Title       string
	URL         string
	// Date is YYYY-MM-DD, empty when unknown.
	Date      string
	CreatedBy *int64
	CreatedAt time.Time
}

// SaveSubject inserts s when s.ID is zero, otherwise updates it within its group.
func (q *Queries) SaveSubject(ctx context.Context, s *Subject) error {
	if s.ID == 0 {
		res, err := q.db.ExecContext(ctx,
			`INSERT INTO subjects (group_id, name, short_name) VALUES (?, ?, ?)`,
			s.GroupID, s.Name, s.ShortName)
		if err != nil {
			return mapErr(err)
		}
		s.ID, err = res.LastInsertId()
		return err
	}
	return expectOne(q.db.ExecContext(ctx,
		`UPDATE subjects SET name = ?, short_name = ? WHERE id = ? AND group_id = ?`,
		s.Name, s.ShortName, s.ID, s.GroupID))
}

// DeleteSubject removes a subject and everything filed under it.
func (q *Queries) DeleteSubject(ctx context.Context, groupID, id int64) error {
	return expectOne(q.db.ExecContext(ctx,
		`DELETE FROM subjects WHERE id = ? AND group_id = ?`, id, groupID))
}

// SubjectByID returns a subject only if it belongs to groupID.
func (q *Queries) SubjectByID(ctx context.Context, groupID, id int64) (*Subject, error) {
	var s Subject
	err := q.db.QueryRowContext(ctx,
		`SELECT id, group_id, name, short_name FROM subjects WHERE id = ? AND group_id = ?`,
		id, groupID,
	).Scan(&s.ID, &s.GroupID, &s.Name, &s.ShortName)
	if err != nil {
		return nil, mapErr(err)
	}
	return &s, nil
}

// SubjectsByGroup returns a group's subjects ordered by name.
func (q *Queries) SubjectsByGroup(ctx context.Context, groupID int64) ([]Subject, error) {
	rows, err := q.db.QueryContext(ctx,
		`SELECT id, group_id, name, short_name FROM subjects WHERE group_id = ? ORDER BY name`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Subject
	for rows.Next() {
		var s Subject
		if err := rows.Scan(&s.ID, &s.GroupID, &s.Name, &s.ShortName); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// SaveClassLink inserts l when l.ID is zero, otherwise updates it within its group.
func (q *Queries) SaveClassLink(ctx context.Context, l *ClassLink) error {
	if l.ID == 0 {
		res, err := q.db.ExecContext(ctx,
			`INSERT INTO class_links (group_id, subject_id, lesson_type, url, note) VALUES (?, ?, ?, ?, ?)`,
			l.GroupID, l.SubjectID, l.LessonType, l.URL, l.Note)
		if err != nil {
			return mapErr(err)
		}
		l.ID, err = res.LastInsertId()
		return err
	}
	return expectOne(q.db.ExecContext(ctx,
		`UPDATE class_links SET subject_id = ?, lesson_type = ?, url = ?, note = ? WHERE id = ? AND group_id = ?`,
		l.SubjectID, l.LessonType, l.URL, l.Note, l.ID, l.GroupID))
}

// DeleteClassLink removes a class link within its group.
func (q *Queries) DeleteClassLink(ctx context.Context, groupID, id int64) error {
	return expectOne(q.db.ExecContext(ctx,
		`DELETE FROM class_links WHERE id = ? AND group_id = ?`, id, groupID))
}

// ClassLinksByGroup returns a group's class links ordered by subject and lesson type.
func (q *Queries) ClassLinksByGroup(ctx context.Context, groupID int64) ([]ClassLink, error) {
	rows, err := q.db.QueryContext(ctx, `
		SELECT l.id, l.group_id, l.subject_id, s.name, l.lesson_type, l.url, l.note
		FROM class_links l JOIN subjects s ON s.id = l.subject_id
		WHERE l.group_id = ?
		ORDER BY s.name, l.lesson_type`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ClassLink
	for rows.Next() {
		var l ClassLink
		if err := rows.Scan(&l.ID, &l.GroupID, &l.SubjectID, &l.SubjectName, &l.LessonType, &l.URL, &l.Note); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// SaveHomework inserts h when h.ID is zero, otherwise updates it within its group.
func (q *Queries) SaveHomework(ctx context.Context, h *Homework, now time.Time) error {
	if h.ID == 0 {
		res, err := q.db.ExecContext(ctx, `
			INSERT INTO homework (group_id, subject_id, title, description_md, due_at, max_points, created_by, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			h.GroupID, h.SubjectID, h.Title, h.DescriptionMD, h.DueAt.Unix(), h.MaxPoints, h.CreatedBy,
			now.Unix(), now.Unix())
		if err != nil {
			return mapErr(err)
		}
		h.ID, err = res.LastInsertId()
		return err
	}
	return expectOne(q.db.ExecContext(ctx, `
		UPDATE homework SET subject_id = ?, title = ?, description_md = ?, due_at = ?, max_points = ?, updated_at = ?
		WHERE id = ? AND group_id = ?`,
		h.SubjectID, h.Title, h.DescriptionMD, h.DueAt.Unix(), h.MaxPoints, now.Unix(), h.ID, h.GroupID))
}

// DeleteHomework removes an assignment within its group.
func (q *Queries) DeleteHomework(ctx context.Context, groupID, id int64) error {
	return expectOne(q.db.ExecContext(ctx,
		`DELETE FROM homework WHERE id = ? AND group_id = ?`, id, groupID))
}

// HomeworkByGroup returns a group's assignments ordered by due date.
func (q *Queries) HomeworkByGroup(ctx context.Context, groupID int64) ([]Homework, error) {
	rows, err := q.db.QueryContext(ctx, `
		SELECT h.id, h.group_id, h.subject_id, s.name, h.title, h.description_md, h.due_at,
		       h.max_points, h.created_by, h.created_at, h.updated_at
		FROM homework h JOIN subjects s ON s.id = h.subject_id
		WHERE h.group_id = ?
		ORDER BY h.due_at, h.id`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Homework
	for rows.Next() {
		var h Homework
		var due, created, updated int64
		var maxPoints *float64
		var createdBy *int64
		if err := rows.Scan(&h.ID, &h.GroupID, &h.SubjectID, &h.SubjectName, &h.Title, &h.DescriptionMD,
			&due, &maxPoints, &createdBy, &created, &updated); err != nil {
			return nil, err
		}
		h.DueAt = time.Unix(due, 0).UTC()
		h.MaxPoints = maxPoints
		h.CreatedBy = createdBy
		h.CreatedAt = time.Unix(created, 0).UTC()
		h.UpdatedAt = time.Unix(updated, 0).UTC()
		out = append(out, h)
	}
	return out, rows.Err()
}

// AddHomeworkLink attaches a link to an assignment, but only if it belongs to groupID.
func (q *Queries) AddHomeworkLink(ctx context.Context, groupID int64, l *HomeworkLink) error {
	res, err := q.db.ExecContext(ctx, `
		INSERT INTO homework_links (homework_id, title, url)
		SELECT id, ?, ? FROM homework WHERE id = ? AND group_id = ?`,
		l.Title, l.URL, l.HomeworkID, groupID)
	if err != nil {
		return mapErr(err)
	}
	if err := expectOne(res, nil); err != nil {
		return err
	}
	l.ID, err = res.LastInsertId()
	return err
}

// DeleteHomeworkLink removes a link if its assignment belongs to groupID.
func (q *Queries) DeleteHomeworkLink(ctx context.Context, groupID, id int64) error {
	return expectOne(q.db.ExecContext(ctx, `
		DELETE FROM homework_links WHERE id = ? AND homework_id IN (SELECT id FROM homework WHERE group_id = ?)`,
		id, groupID))
}

// HomeworkLinksByGroup returns the links of every assignment in a group.
func (q *Queries) HomeworkLinksByGroup(ctx context.Context, groupID int64) ([]HomeworkLink, error) {
	rows, err := q.db.QueryContext(ctx, `
		SELECT l.id, l.homework_id, l.title, l.url
		FROM homework_links l JOIN homework h ON h.id = l.homework_id
		WHERE h.group_id = ?
		ORDER BY l.id`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HomeworkLink
	for rows.Next() {
		var l HomeworkLink
		if err := rows.Scan(&l.ID, &l.HomeworkID, &l.Title, &l.URL); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// SaveNote inserts n when n.ID is zero, otherwise updates it within its group.
func (q *Queries) SaveNote(ctx context.Context, n *Note, now time.Time) error {
	if n.ID == 0 {
		res, err := q.db.ExecContext(ctx, `
			INSERT INTO notes (group_id, title, body_md, pinned, created_by, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			n.GroupID, n.Title, n.BodyMD, n.Pinned, n.CreatedBy, now.Unix(), now.Unix())
		if err != nil {
			return mapErr(err)
		}
		n.ID, err = res.LastInsertId()
		return err
	}
	return expectOne(q.db.ExecContext(ctx, `
		UPDATE notes SET title = ?, body_md = ?, pinned = ?, updated_at = ? WHERE id = ? AND group_id = ?`,
		n.Title, n.BodyMD, n.Pinned, now.Unix(), n.ID, n.GroupID))
}

// DeleteNote removes a note within its group.
func (q *Queries) DeleteNote(ctx context.Context, groupID, id int64) error {
	return expectOne(q.db.ExecContext(ctx,
		`DELETE FROM notes WHERE id = ? AND group_id = ?`, id, groupID))
}

// NotesByGroup returns a group's notes, pinned first, newest first.
func (q *Queries) NotesByGroup(ctx context.Context, groupID int64) ([]Note, error) {
	rows, err := q.db.QueryContext(ctx, `
		SELECT id, group_id, title, body_md, pinned, created_by, created_at, updated_at
		FROM notes WHERE group_id = ?
		ORDER BY pinned DESC, created_at DESC, id DESC`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Note
	for rows.Next() {
		var n Note
		var created, updated int64
		if err := rows.Scan(&n.ID, &n.GroupID, &n.Title, &n.BodyMD, &n.Pinned, &n.CreatedBy, &created, &updated); err != nil {
			return nil, err
		}
		n.CreatedAt = time.Unix(created, 0).UTC()
		n.UpdatedAt = time.Unix(updated, 0).UTC()
		out = append(out, n)
	}
	return out, rows.Err()
}

// SaveResource inserts r when r.ID is zero, otherwise updates it within its group.
func (q *Queries) SaveResource(ctx context.Context, r *ResourceLink) error {
	date := nullString(r.Date)
	if r.ID == 0 {
		res, err := q.db.ExecContext(ctx, `
			INSERT INTO resource_links (group_id, subject_id, kind, title, url, date, created_by, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			r.GroupID, r.SubjectID, r.Kind, r.Title, r.URL, date, r.CreatedBy, r.CreatedAt.Unix())
		if err != nil {
			return mapErr(err)
		}
		r.ID, err = res.LastInsertId()
		return err
	}
	return expectOne(q.db.ExecContext(ctx, `
		UPDATE resource_links SET subject_id = ?, kind = ?, title = ?, url = ?, date = ? WHERE id = ? AND group_id = ?`,
		r.SubjectID, r.Kind, r.Title, r.URL, date, r.ID, r.GroupID))
}

// DeleteResource removes a recording or solution within its group.
func (q *Queries) DeleteResource(ctx context.Context, groupID, id int64) error {
	return expectOne(q.db.ExecContext(ctx,
		`DELETE FROM resource_links WHERE id = ? AND group_id = ?`, id, groupID))
}

// ResourcesByGroup returns a group's recordings and solutions, newest date first.
func (q *Queries) ResourcesByGroup(ctx context.Context, groupID int64) ([]ResourceLink, error) {
	rows, err := q.db.QueryContext(ctx, `
		SELECT r.id, r.group_id, r.subject_id, s.name, r.kind, r.title, r.url, r.date, r.created_by, r.created_at
		FROM resource_links r JOIN subjects s ON s.id = r.subject_id
		WHERE r.group_id = ?
		ORDER BY s.name, r.kind, r.date DESC, r.id DESC`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ResourceLink
	for rows.Next() {
		var r ResourceLink
		var date *string
		var created int64
		if err := rows.Scan(&r.ID, &r.GroupID, &r.SubjectID, &r.SubjectName, &r.Kind, &r.Title, &r.URL,
			&date, &r.CreatedBy, &created); err != nil {
			return nil, err
		}
		if date != nil {
			r.Date = *date
		}
		r.CreatedAt = time.Unix(created, 0).UTC()
		out = append(out, r)
	}
	return out, rows.Err()
}

func nullString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
