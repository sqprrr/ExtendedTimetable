package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/sqprrr/ExtendedTimetable/internal/service"
	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

func TestSubjectTeachersAndDLPage(t *testing.T) {
	f := setup(t)
	lead := f.leader(t, "lead")
	sub, err := f.svc.CreateSubject(lead, f.group.ID, service.SubjectInput{
		Name: "Physics", Lecturer: "  Bondarenko O. V. ", Instructor: "Lytvynenko A. S.", DLURL: " https://dl.nure.ua/course/view.php?id=1 ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if sub.Lecturer != "Bondarenko O. V." || sub.DLURL != "https://dl.nure.ua/course/view.php?id=1" {
		t.Fatalf("saved subject: %+v", sub)
	}
	got, err := f.svc.Subject(lead, f.group.ID, sub.ID)
	if err != nil || got.Instructor != "Lytvynenko A. S." || got.DLURL != sub.DLURL {
		t.Fatalf("read back: %+v %v", got, err)
	}

	_, err = f.svc.UpdateSubject(lead, f.group.ID, sub.ID, service.SubjectInput{Name: "Physics", DLURL: "dl.nure.ua"})
	wantInputError(t, err, "dl_url")
	_, err = f.svc.UpdateSubject(lead, f.group.ID, sub.ID, service.SubjectInput{Name: "Physics", Lecturer: "a\nb"})
	wantInputError(t, err, "lecturer")

	// Clearing the optional fields is allowed.
	got, err = f.svc.UpdateSubject(lead, f.group.ID, sub.ID, service.SubjectInput{Name: "Physics"})
	if err != nil || got.Lecturer != "" || got.DLURL != "" {
		t.Fatalf("cleared: %+v %v", got, err)
	}
}

func TestSubjectPage(t *testing.T) {
	f := setup(t)
	lead := f.leader(t, "lead")
	phys, chem := f.subject(t, lead, "Physics"), f.subject(t, lead, "Chemistry")
	link := func(sub int64, lt store.LessonType) {
		if _, err := f.svc.CreateClassLink(lead, f.group.ID, service.ClassLinkInput{SubjectID: sub, LessonType: lt, URL: "https://meet.example/" + string(lt)}); err != nil {
			t.Fatal(err)
		}
	}
	link(phys.ID, store.LessonLab)
	link(phys.ID, store.LessonLecture)
	link(chem.ID, store.LessonPractice)
	f.homework(t, lead, phys.ID, "Lab 1", nil, nil)
	f.homework(t, lead, chem.ID, "Other", nil, nil)
	rec, err := f.svc.CreateResourceLink(lead, f.group.ID, service.ResourceLinkInput{
		SubjectID: phys.ID, Kind: store.ResourceRecording, Title: "Lecture 1", URL: "https://youtu.be/x", LessonType: store.LessonLecture,
	})
	if err != nil || rec.LessonType != store.LessonLecture {
		t.Fatalf("recording: %+v %v", rec, err)
	}
	if _, err := f.svc.CreateResourceLink(lead, f.group.ID, service.ResourceLinkInput{
		SubjectID: chem.ID, Kind: store.ResourceSolution, Title: "Other", URL: "https://example.org",
	}); err != nil {
		t.Fatal(err)
	}
	_, err = f.svc.CreateResourceLink(lead, f.group.ID, service.ResourceLinkInput{
		SubjectID: phys.ID, Kind: store.ResourceRecording, Title: "Bad", URL: "https://example.org", LessonType: "seminar",
	})
	wantInputError(t, err, "lesson_type")

	stud := f.as(t, f.register(t, "stud"))
	p, err := f.svc.SubjectPage(stud, f.group.ID, phys.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.ClassLinks) != 2 || p.ClassLinks[0].LessonType != store.LessonLecture || p.ClassLinks[1].LessonType != store.LessonLab {
		t.Fatalf("class links (lectures first): %+v", p.ClassLinks)
	}
	if len(p.Homework) != 1 || p.Homework[0].Title != "Lab 1" || !p.Homework[0].Tracked {
		t.Fatalf("homework: %+v", p.Homework)
	}
	if len(p.Resources) != 1 || p.Resources[0].Title != "Lecture 1" {
		t.Fatalf("resources: %+v", p.Resources)
	}

	// Another group's member cannot open it, and ids are scoped to the group.
	if _, err := f.svc.AdminCreateGroup(context.Background(), "OTHER-1", "", nil); err != nil {
		t.Fatal(err)
	}
	outsider := f.as(t, f.registerInto(t, "OTHER-1", "olga"))
	if _, err := f.svc.SubjectPage(outsider, f.group.ID, phys.ID); !errors.Is(err, service.ErrForbidden) {
		t.Fatalf("outsider: %v", err)
	}
	other, err := f.svc.Group(outsider, "OTHER-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.SubjectPage(outsider, other.ID, phys.ID); !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("subject of another group: %v", err)
	}
}
