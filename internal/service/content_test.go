package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sqprrr/ExtendedTimetable/internal/service"
	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

// leaderCtx registers a student, promotes them to leader of the fixture group and returns their context.
func (f *fixture) leaderCtx(t *testing.T, username string) context.Context {
	t.Helper()
	f.register(t, username)
	if err := f.svc.AdminSetRole(context.Background(), username, f.group.Code, store.RoleLeader); err != nil {
		t.Fatal(err)
	}
	sess, err := f.svc.Login(context.Background(), service.LoginInput{Username: username, Password: "correct horse", ClientIP: "login-" + username})
	if err != nil {
		t.Fatal(err)
	}
	return f.as(t, sess)
}

func (f *fixture) superadminCtx(t *testing.T) context.Context {
	t.Helper()
	ctx := context.Background()
	if err := f.svc.AdminCreateSuperadmin(ctx, "root", "correct horse"); err != nil {
		t.Fatal(err)
	}
	sess, err := f.svc.Login(ctx, service.LoginInput{Username: "root", Password: "correct horse", ClientIP: "root"})
	if err != nil {
		t.Fatal(err)
	}
	return f.as(t, sess)
}

func requireErr(t *testing.T, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("got %v, want %v", err, want)
	}
}

func TestGroupPageVisibility(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	student := f.as(t, f.register(t, "stud"))
	other, err := f.svc.AdminCreateGroup(ctx, "OTHER-1", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := f.svc.Register(ctx, service.RegisterInput{
		Username: "outsider", Password: "correct horse", GroupCode: other.Code, ClientIP: "outsider",
	})
	if err != nil {
		t.Fatal(err)
	}
	outsider := f.as(t, sess)

	if _, err := f.svc.GroupPage(outsider, f.group.Code); !errors.Is(err, service.ErrForbidden) {
		t.Errorf("non-member view: got %v, want ErrForbidden", err)
	}
	if _, err := f.svc.GroupPage(student, f.group.Code); err != nil {
		t.Fatalf("member view: %v", err)
	}
	if _, err := f.svc.GroupPage(student, "NO-SUCH"); !errors.Is(err, service.ErrNotFound) {
		t.Errorf("unknown group: got %v, want ErrNotFound", err)
	}
	if _, err := f.svc.GroupPage(context.Background(), f.group.Code); !errors.Is(err, service.ErrUnauthenticated) {
		t.Errorf("anonymous view: got %v, want ErrUnauthenticated", err)
	}
	page, err := f.svc.GroupPage(student, f.group.Code)
	if err != nil {
		t.Fatal(err)
	}
	if page.CanManage {
		t.Error("student should not manage the group")
	}
}

func TestStudentCannotEditContent(t *testing.T) {
	f := setup(t)
	ctx := f.as(t, f.register(t, "stud"))
	code := f.group.Code

	requireErr(t, f.svc.SaveSubject(ctx, code, 0, service.SubjectInput{Name: "Math"}), service.ErrForbidden)
	requireErr(t, f.svc.SaveNote(ctx, code, 0, service.NoteInput{Title: "hi"}), service.ErrForbidden)
	requireErr(t, f.svc.DeleteSubject(ctx, code, 1), service.ErrForbidden)
}

func TestLeaderOfOtherGroupCannotEdit(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	other, err := f.svc.AdminCreateGroup(ctx, "OTHER-1", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	leader := f.leaderCtx(t, "leader")
	requireErr(t, f.svc.SaveNote(leader, other.Code, 0, service.NoteInput{Title: "hi"}), service.ErrForbidden)
	if err := f.svc.SaveNote(leader, f.group.Code, 0, service.NoteInput{Title: "hi"}); err != nil {
		t.Fatalf("own group: %v", err)
	}
}

func TestLeaderCRUD(t *testing.T) {
	f := setup(t)
	ctx := f.leaderCtx(t, "leader")
	code := f.group.Code

	if err := f.svc.SaveSubject(ctx, code, 0, service.SubjectInput{Name: "Mathematics", ShortName: "Math"}); err != nil {
		t.Fatal(err)
	}
	page, err := f.svc.GroupPage(ctx, code)
	if err != nil {
		t.Fatal(err)
	}
	if !page.CanManage || len(page.Subjects) != 1 {
		t.Fatalf("page: canManage=%v subjects=%d", page.CanManage, len(page.Subjects))
	}
	subjID := page.Subjects[0].ID

	if err := f.svc.SaveSubject(ctx, code, 0, service.SubjectInput{Name: "Mathematics"}); err == nil {
		t.Error("duplicate subject name should fail")
	}
	if err := f.svc.SaveSubject(ctx, code, subjID, service.SubjectInput{Name: "Algebra"}); err != nil {
		t.Fatalf("rename subject: %v", err)
	}

	if err := f.svc.SaveClassLink(ctx, code, 0, service.ClassLinkInput{
		SubjectID: subjID, LessonType: "lecture", URL: "https://meet.example.com/a",
	}); err != nil {
		t.Fatal(err)
	}

	due := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	points := 10.0
	if err := f.svc.SaveHomework(ctx, code, 0, service.HomeworkInput{
		SubjectID: subjID, Title: "HW 1", DescriptionMD: "**do** it", DueAt: due, MaxPoints: &points,
	}); err != nil {
		t.Fatal(err)
	}
	page, _ = f.svc.GroupPage(ctx, code)
	hw := page.Homework[0]
	if !hw.Overdue || hw.SubjectName != "Algebra" || hw.MaxPoints == nil || *hw.MaxPoints != 10 {
		t.Fatalf("homework = %+v", hw)
	}
	if err := f.svc.AddHomeworkLink(ctx, code, hw.ID, service.HomeworkLinkInput{Title: "Task", URL: "https://example.com/t"}); err != nil {
		t.Fatal(err)
	}
	page, _ = f.svc.GroupPage(ctx, code)
	if len(page.Homework[0].Links) != 1 {
		t.Fatalf("links = %d, want 1", len(page.Homework[0].Links))
	}

	if err := f.svc.SaveNote(ctx, code, 0, service.NoteInput{Title: "Welcome", BodyMD: "hi", Pinned: true}); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.SaveResource(ctx, code, 0, service.ResourceInput{
		SubjectID: subjID, Kind: "recording", Title: "Lecture 1", URL: "https://youtu.be/x", Date: time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}
	page, _ = f.svc.GroupPage(ctx, code)
	if len(page.Notes) != 1 || len(page.Resources) != 1 || page.Resources[0].Date != "2026-09-02" {
		t.Fatalf("notes=%d resources=%+v", len(page.Notes), page.Resources)
	}

	// Deleting the subject cascades to everything filed under it.
	if err := f.svc.DeleteSubject(ctx, code, subjID); err != nil {
		t.Fatal(err)
	}
	page, _ = f.svc.GroupPage(ctx, code)
	if len(page.Homework) != 0 || len(page.ClassLinks) != 0 || len(page.Resources) != 0 || len(page.Subjects) != 0 {
		t.Errorf("cascade delete left content behind: %+v", page)
	}
}

func TestLeaderInputValidation(t *testing.T) {
	f := setup(t)
	ctx := f.leaderCtx(t, "leader")
	code := f.group.Code
	if err := f.svc.SaveSubject(ctx, code, 0, service.SubjectInput{Name: "Math"}); err != nil {
		t.Fatal(err)
	}
	page, _ := f.svc.GroupPage(ctx, code)
	subjID := page.Subjects[0].ID

	bad := map[string]error{
		"javascript url":  f.svc.SaveClassLink(ctx, code, 0, service.ClassLinkInput{SubjectID: subjID, LessonType: "lab", URL: "javascript:alert(1)"}),
		"bad lesson":      f.svc.SaveClassLink(ctx, code, 0, service.ClassLinkInput{SubjectID: subjID, LessonType: "seminar", URL: "https://x.example"}),
		"foreign subject": f.svc.SaveClassLink(ctx, code, 0, service.ClassLinkInput{SubjectID: 9999, LessonType: "lab", URL: "https://x.example"}),
		"no due date":     f.svc.SaveHomework(ctx, code, 0, service.HomeworkInput{SubjectID: subjID, Title: "HW"}),
		"zero points":     f.svc.SaveHomework(ctx, code, 0, service.HomeworkInput{SubjectID: subjID, Title: "HW", DueAt: time.Now(), MaxPoints: ptr(0)}),
		"empty note":      f.svc.SaveNote(ctx, code, 0, service.NoteInput{Title: "   "}),
		"resource kind":   f.svc.SaveResource(ctx, code, 0, service.ResourceInput{SubjectID: subjID, Kind: "video", Title: "x", URL: "https://x.example"}),
	}
	for name, err := range bad {
		var ie *service.InputError
		if !errors.As(err, &ie) {
			t.Errorf("%s: got %v, want InputError", name, err)
		}
	}
}

func TestHomeworkLinkMustBelongToGroup(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	other, err := f.svc.AdminCreateGroup(ctx, "OTHER-1", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	leader := f.leaderCtx(t, "leader")
	if err := f.svc.SaveSubject(leader, f.group.Code, 0, service.SubjectInput{Name: "Math"}); err != nil {
		t.Fatal(err)
	}
	page, _ := f.svc.GroupPage(leader, f.group.Code)
	subjID := page.Subjects[0].ID
	if err := f.svc.SaveHomework(leader, f.group.Code, 0, service.HomeworkInput{
		SubjectID: subjID, Title: "HW", DueAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	page, _ = f.svc.GroupPage(leader, f.group.Code)
	hwID := page.Homework[0].ID

	// The homework belongs to KIUKI, so a link added through another group must not land.
	admin := f.superadminCtx(t)
	requireErr(t, f.svc.AddHomeworkLink(admin, other.Code, hwID, service.HomeworkLinkInput{Title: "x", URL: "https://x.example"}), service.ErrNotFound)
}

func TestSuperadminCanEditAnyGroup(t *testing.T) {
	f := setup(t)
	admin := f.superadminCtx(t)
	if err := f.svc.SaveNote(admin, f.group.Code, 0, service.NoteInput{Title: "from root"}); err != nil {
		t.Fatalf("superadmin note: %v", err)
	}
	page, err := f.svc.GroupPage(admin, f.group.Code)
	if err != nil {
		t.Fatal(err)
	}
	if !page.CanManage || len(page.Notes) != 1 {
		t.Errorf("superadmin page: canManage=%v notes=%d", page.CanManage, len(page.Notes))
	}
}

func ptr(f float64) *float64 { return &f }
