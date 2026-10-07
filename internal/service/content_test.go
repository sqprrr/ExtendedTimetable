package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sqprrr/ExtendedTimetable/internal/service"
	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

// leader registers username and promotes them to leader of the fixture group.
func (f *fixture) leader(t *testing.T, username string) context.Context {
	t.Helper()
	sess := f.register(t, username)
	if err := f.svc.AdminSetRole(context.Background(), username, f.group.Code, store.RoleLeader); err != nil {
		t.Fatal(err)
	}
	return f.as(t, sess)
}

func wantInputError(t *testing.T, err error, field string) {
	t.Helper()
	var ie *service.InputError
	if !errors.As(err, &ie) || ie.Field != field {
		t.Fatalf("got %v, want InputError on %q", err, field)
	}
}

func TestLeaderManagesContentStudentsRead(t *testing.T) {
	f := setup(t)
	lead := f.leader(t, "lead")
	student := f.as(t, f.register(t, "stud"))
	gid := f.group.ID

	sub, err := f.svc.CreateSubject(lead, gid, service.SubjectInput{Name: " Вища математика ", ShortName: "ВМ"})
	if err != nil {
		t.Fatal(err)
	}
	if sub.Name != "Вища математика" {
		t.Errorf("name not trimmed: %q", sub.Name)
	}
	if _, err := f.svc.CreateSubject(lead, gid, service.SubjectInput{Name: "вища МАТЕМАТИКА"}); err == nil {
		t.Error("duplicate subject name (case-insensitive) should fail")
	}

	link, err := f.svc.CreateClassLink(lead, gid, service.ClassLinkInput{
		SubjectID: sub.ID, LessonType: store.LessonLecture, URL: "https://meet.google.com/abc-defg-hij",
	})
	if err != nil {
		t.Fatal(err)
	}
	due := time.Now().Add(48 * time.Hour).Truncate(time.Second)
	max := 10.0
	hw, err := f.svc.CreateHomework(lead, gid, service.HomeworkInput{
		SubjectID: sub.ID, Title: "Lab 1", Description: "Solve **all**", DueAt: &due, MaxPoints: &max,
		Links: []service.LinkInput{{Title: "Manual", URL: "https://example.com/manual.pdf"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.CreateNote(lead, gid, service.NoteInput{Title: "Welcome", Body: "Hi", Pinned: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.CreateResourceLink(lead, gid, service.ResourceLinkInput{
		SubjectID: sub.ID, Kind: store.ResourceRecording, Title: "Lecture 1", URL: "https://youtu.be/x", Date: "2026-09-01",
	}); err != nil {
		t.Fatal(err)
	}

	// Students read everything.
	if links, err := f.svc.ClassLinks(student, gid); err != nil || len(links) != 1 || links[0].SubjectName != "Вища математика" {
		t.Fatalf("class links: %v %+v", err, links)
	}
	got, err := f.svc.Homework(student, gid, hw.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.DueAt.Equal(due) || *got.MaxPoints != 10 || len(got.Links) != 1 || got.Links[0].Title != "Manual" || got.Overdue {
		t.Fatalf("homework: %+v links=%+v", got, got.Links)
	}
	if notes, err := f.svc.Notes(student, gid); err != nil || len(notes) != 1 || !notes[0].Pinned {
		t.Fatalf("notes: %v %+v", err, notes)
	}
	if res, err := f.svc.ResourceLinks(student, gid); err != nil || len(res) != 1 || res[0].Date != "2026-09-01" {
		t.Fatalf("resources: %v %+v", err, res)
	}

	// ...but cannot change anything.
	writes := map[string]error{}
	_, writes["create subject"] = f.svc.CreateSubject(student, gid, service.SubjectInput{Name: "X"})
	_, writes["update subject"] = f.svc.UpdateSubject(student, gid, sub.ID, service.SubjectInput{Name: "X"})
	writes["delete subject"] = f.svc.DeleteSubject(student, gid, sub.ID)
	writes["delete link"] = f.svc.DeleteClassLink(student, gid, link.ID)
	_, writes["create homework"] = f.svc.CreateHomework(student, gid, service.HomeworkInput{SubjectID: sub.ID, Title: "X"})
	writes["delete homework"] = f.svc.DeleteHomework(student, gid, hw.ID)
	_, writes["create note"] = f.svc.CreateNote(student, gid, service.NoteInput{Title: "X"})
	_, writes["create resource"] = f.svc.CreateResourceLink(student, gid, service.ResourceLinkInput{
		SubjectID: sub.ID, Kind: store.ResourceSolution, Title: "X", URL: "https://example.com"})
	for name, err := range writes {
		if !errors.Is(err, service.ErrForbidden) {
			t.Errorf("student %s: got %v, want ErrForbidden", name, err)
		}
	}
}

func TestGroupsAreIsolated(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	other, err := f.svc.AdminCreateGroup(ctx, "OTHER-1", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	lead := f.leader(t, "lead")
	sub, err := f.svc.CreateSubject(lead, f.group.ID, service.SubjectInput{Name: "Physics"})
	if err != nil {
		t.Fatal(err)
	}
	note, err := f.svc.CreateNote(lead, f.group.ID, service.NoteInput{Title: "Ours"})
	if err != nil {
		t.Fatal(err)
	}

	// A leader of another group can neither read nor write this group.
	sess, err := f.svc.Register(ctx, service.RegisterInput{Username: "olead", Password: "correct horse", GroupCode: "OTHER-1", ClientIP: "o"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.AdminSetRole(ctx, "olead", "OTHER-1", store.RoleLeader); err != nil {
		t.Fatal(err)
	}
	olead := f.as(t, sess)
	if _, err := f.svc.Notes(olead, f.group.ID); !errors.Is(err, service.ErrForbidden) {
		t.Errorf("read other group's notes: %v", err)
	}
	if err := f.svc.DeleteNote(olead, f.group.ID, note.ID); !errors.Is(err, service.ErrForbidden) {
		t.Errorf("delete other group's note: %v", err)
	}
	if _, err := f.svc.Group(olead, f.group.Code); !errors.Is(err, service.ErrForbidden) {
		t.Errorf("open other group: %v", err)
	}

	// Ids from another group are not found, even through a group one manages.
	if err := f.svc.DeleteNote(olead, other.ID, note.ID); !errors.Is(err, service.ErrNotFound) {
		t.Errorf("delete note through own group: %v", err)
	}
	_, err = f.svc.CreateHomework(olead, other.ID, service.HomeworkInput{SubjectID: sub.ID, Title: "Steal"})
	wantInputError(t, err, "subject_id")

	// A superadmin manages every group without being a member.
	if err := f.svc.AdminCreateSuperadmin(ctx, "root", "correct horse"); err != nil {
		t.Fatal(err)
	}
	rs, err := f.svc.Login(ctx, service.LoginInput{Username: "root", Password: "correct horse", ClientIP: "r"})
	if err != nil {
		t.Fatal(err)
	}
	root := f.as(t, rs)
	if g, err := f.svc.Group(root, "kiuki-25-3"); err != nil || !g.CanManage {
		t.Fatalf("superadmin group: %v %+v", err, g)
	}
	if err := f.svc.DeleteNote(root, f.group.ID, note.ID); err != nil {
		t.Errorf("superadmin delete: %v", err)
	}
}

func TestNonMembersAndAnonymous(t *testing.T) {
	f := setup(t)
	if _, err := f.svc.Subjects(context.Background(), f.group.ID); !errors.Is(err, service.ErrUnauthenticated) {
		t.Errorf("anonymous: %v", err)
	}
	if _, err := f.svc.Group(f.as(t, f.register(t, "anna")), "NOPE"); !errors.Is(err, service.ErrNotFound) {
		t.Errorf("unknown group: %v", err)
	}
}

func TestContentValidation(t *testing.T) {
	f := setup(t)
	lead := f.leader(t, "lead")
	gid := f.group.ID
	sub, err := f.svc.CreateSubject(lead, gid, service.SubjectInput{Name: "Physics"})
	if err != nil {
		t.Fatal(err)
	}

	_, err = f.svc.CreateSubject(lead, gid, service.SubjectInput{Name: "  "})
	wantInputError(t, err, "name")

	for _, u := range []string{"", "javascript:alert(1)", "ftp://example.com", "example.com", "https://"} {
		_, err = f.svc.CreateClassLink(lead, gid, service.ClassLinkInput{SubjectID: sub.ID, LessonType: store.LessonLab, URL: u})
		wantInputError(t, err, "url")
	}
	_, err = f.svc.CreateClassLink(lead, gid, service.ClassLinkInput{SubjectID: sub.ID, LessonType: "seminar", URL: "https://x.org"})
	wantInputError(t, err, "lesson_type")

	_, err = f.svc.CreateHomework(lead, gid, service.HomeworkInput{SubjectID: sub.ID})
	wantInputError(t, err, "title")
	zero, huge := 0.0, 12000.0
	_, err = f.svc.CreateHomework(lead, gid, service.HomeworkInput{SubjectID: sub.ID, Title: "T", MaxPoints: &zero})
	wantInputError(t, err, "max_points")
	_, err = f.svc.CreateHomework(lead, gid, service.HomeworkInput{SubjectID: sub.ID, Title: "T", MaxPoints: &huge})
	wantInputError(t, err, "max_points")
	if !strings.Contains(err.Error(), "at most 10000") {
		t.Errorf("max points over the limit: %v", err)
	}
	_, err = f.svc.CreateHomework(lead, gid, service.HomeworkInput{SubjectID: sub.ID, Title: "two\nlines"})
	wantInputError(t, err, "title")
	_, err = f.svc.CreateHomework(lead, gid, service.HomeworkInput{SubjectID: sub.ID, Title: "T",
		Links: []service.LinkInput{{Title: "a\nb", URL: "https://x.org"}}})
	wantInputError(t, err, "links")
	_, err = f.svc.CreateHomework(lead, gid, service.HomeworkInput{SubjectID: sub.ID, Title: "T",
		Links: []service.LinkInput{{URL: "javascript:alert(1)"}}})
	wantInputError(t, err, "links")
	_, err = f.svc.CreateHomework(lead, gid, service.HomeworkInput{SubjectID: 9999, Title: "T"})
	wantInputError(t, err, "subject_id")

	_, err = f.svc.CreateResourceLink(lead, gid, service.ResourceLinkInput{
		SubjectID: sub.ID, Kind: store.ResourceSolution, Title: "T", URL: "https://x.org", Date: "01.09.2026"})
	wantInputError(t, err, "date")
	_, err = f.svc.CreateResourceLink(lead, gid, service.ResourceLinkInput{
		SubjectID: sub.ID, Kind: "file", Title: "T", URL: "https://x.org"})
	wantInputError(t, err, "kind")
}

func TestDeleteSubjectInUse(t *testing.T) {
	f := setup(t)
	lead := f.leader(t, "lead")
	gid := f.group.ID
	sub, err := f.svc.CreateSubject(lead, gid, service.SubjectInput{Name: "Physics"})
	if err != nil {
		t.Fatal(err)
	}
	hw, err := f.svc.CreateHomework(lead, gid, service.HomeworkInput{SubjectID: sub.ID, Title: "Lab"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.DeleteSubject(lead, gid, sub.ID); !errors.Is(err, service.ErrSubjectInUse) {
		t.Fatalf("delete used subject: %v", err)
	}
	if err := f.svc.DeleteHomework(lead, gid, hw.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.DeleteSubject(lead, gid, sub.ID); err != nil {
		t.Fatalf("delete unused subject: %v", err)
	}
	if err := f.svc.DeleteSubject(lead, gid, sub.ID); !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("delete twice: %v", err)
	}
}

func TestHomeworkUpdateAndOrder(t *testing.T) {
	f := setup(t)
	lead := f.leader(t, "lead")
	gid := f.group.ID
	sub, err := f.svc.CreateSubject(lead, gid, service.SubjectInput{Name: "Physics"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	past, soon, later := now.Add(-time.Hour), now.Add(time.Hour), now.Add(48*time.Hour)
	mk := func(title string, due *time.Time) *service.Homework {
		hw, err := f.svc.CreateHomework(lead, gid, service.HomeworkInput{SubjectID: sub.ID, Title: title, DueAt: due,
			Links: []service.LinkInput{{URL: "https://a.example"}, {URL: "https://b.example"}}})
		if err != nil {
			t.Fatal(err)
		}
		return hw
	}
	mk("no deadline", nil)
	mk("later", &later)
	mk("past", &past)
	hw := mk("soon", &soon)
	if hw.SubjectName != "Physics" || len(hw.Links) != 2 {
		t.Fatalf("create should return the full assignment: %+v", hw)
	}

	list, err := f.svc.HomeworkList(lead, gid, service.HomeworkFilter{})
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, h := range list {
		titles = append(titles, h.Title)
	}
	want := []string{"past", "soon", "later", "no deadline"}
	for i := range want {
		if i >= len(titles) || titles[i] != want[i] {
			t.Fatalf("order = %v, want %v", titles, want)
		}
	}
	if !list[0].Overdue || list[1].Overdue || list[3].Overdue {
		t.Errorf("overdue flags wrong: %+v", list)
	}

	if _, err := f.svc.UpdateHomework(lead, gid, hw.ID, service.HomeworkInput{SubjectID: sub.ID, Title: "renamed",
		Links: []service.LinkInput{{Title: "only", URL: "https://c.example"}}}); err != nil {
		t.Fatal(err)
	}
	got, err := f.svc.Homework(lead, gid, hw.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "renamed" || got.DueAt != nil || len(got.Links) != 1 || got.Links[0].URL != "https://c.example" {
		t.Fatalf("after update: %+v links=%+v", got, got.Links)
	}
	if _, err := f.svc.UpdateHomework(lead, gid, 9999, service.HomeworkInput{SubjectID: sub.ID, Title: "x"}); !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("update missing: %v", err)
	}
}

func TestNotesPinnedFirst(t *testing.T) {
	f := setup(t)
	lead := f.leader(t, "lead")
	gid := f.group.ID
	for _, in := range []service.NoteInput{{Title: "old"}, {Title: "pinned", Pinned: true}, {Title: "new"}} {
		if _, err := f.svc.CreateNote(lead, gid, in); err != nil {
			t.Fatal(err)
		}
	}
	notes, err := f.svc.Notes(lead, gid)
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 3 || notes[0].Title != "pinned" || notes[1].Title != "new" || notes[2].Title != "old" {
		t.Fatalf("notes order: %v %v %v", notes[0].Title, notes[1].Title, notes[2].Title)
	}
	n, err := f.svc.UpdateNote(lead, gid, notes[0].ID, service.NoteInput{Title: "unpinned"})
	if err != nil || n.Pinned || n.Title != "unpinned" {
		t.Fatalf("update note: %v %+v", err, n)
	}
}

func TestGroupOverview(t *testing.T) {
	f := setup(t)
	lead := f.leader(t, "lead")
	gid := f.group.ID
	sub, err := f.svc.CreateSubject(lead, gid, service.SubjectInput{Name: "Physics"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for title, due := range map[string]time.Duration{"long ago": -30 * 24 * time.Hour, "yesterday": -24 * time.Hour, "tomorrow": 24 * time.Hour} {
		d := now.Add(due)
		if _, err := f.svc.CreateHomework(lead, gid, service.HomeworkInput{SubjectID: sub.ID, Title: title, DueAt: &d}); err != nil {
			t.Fatal(err)
		}
	}
	for i, title := range []string{"n1", "n2", "pinned", "n3", "n4"} {
		if _, err := f.svc.CreateNote(lead, gid, service.NoteInput{Title: title, Pinned: i == 2}); err != nil {
			t.Fatal(err)
		}
	}

	ov, err := f.svc.GroupOverview(f.as(t, f.register(t, "stud")), gid)
	if err != nil {
		t.Fatal(err)
	}
	if len(ov.Homework) != 2 || ov.Homework[0].Title != "yesterday" || !ov.Homework[0].Overdue || ov.Homework[1].Title != "tomorrow" {
		t.Fatalf("overview homework: %+v", ov.Homework)
	}
	var notes []string
	for _, n := range ov.Notes {
		notes = append(notes, n.Title)
	}
	if strings.Join(notes, ",") != "pinned,n4,n3,n2" {
		t.Fatalf("overview notes = %v", notes)
	}
}

func TestSubjectHue(t *testing.T) {
	if got := service.Hue(1, ""); got != "teal" {
		t.Errorf("default hue of subject 1 = %q, want teal", got)
	}
	if got := service.Hue(9, "rose"); got != "rose" {
		t.Errorf("chosen hue = %q, want rose", got)
	}
	if got := service.Hue(8, "pink"); got != "blue" {
		t.Errorf("unknown stored hue = %q, want the default blue", got)
	}

	f := setup(t)
	lead := f.leader(t, "lead")
	gid := f.group.ID
	sub, err := f.svc.CreateSubject(lead, gid, service.SubjectInput{Name: "Physics", Hue: "violet"})
	if err != nil {
		t.Fatal(err)
	}
	hw, err := f.svc.CreateHomework(lead, gid, service.HomeworkInput{SubjectID: sub.ID, Title: "Lab 1"})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := f.svc.Homework(lead, gid, hw.ID); got.SubjectHue != "violet" {
		t.Errorf("homework carries hue %q, want violet", got.SubjectHue)
	}
	_, err = f.svc.UpdateSubject(lead, gid, sub.ID, service.SubjectInput{Name: "Physics", Hue: "pink"})
	wantInputError(t, err, "hue")
	if _, err := f.svc.UpdateSubject(lead, gid, sub.ID, service.SubjectInput{Name: "Physics"}); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.svc.Subject(lead, gid, sub.ID); got.Hue != "" {
		t.Errorf("an empty hue should go back to the default, got %q", got.Hue)
	}
}
