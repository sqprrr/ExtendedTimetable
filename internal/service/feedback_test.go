package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sqprrr/ExtendedTimetable/internal/service"
	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

func (f *fixture) superadmin(t *testing.T) context.Context {
	t.Helper()
	ctx := context.Background()
	if err := f.svc.AdminCreateSuperadmin(ctx, "root", "correct horse"); err != nil {
		t.Fatal(err)
	}
	sess, err := f.svc.Login(ctx, service.LoginInput{Username: "root", Password: "correct horse", ClientIP: "r"})
	if err != nil {
		t.Fatal(err)
	}
	return f.as(t, sess)
}

func TestFeedback(t *testing.T) {
	f := setup(t)
	alice := f.as(t, f.register(t, "alice"))
	bob := f.as(t, f.register(t, "bob"))
	root := f.superadmin(t)

	review, err := f.svc.SendFeedback(alice, service.FeedbackInput{Kind: store.FeedbackReview, Rating: ptr(int64(4)), Message: "  Nice site\r\nthanks  "})
	if err != nil {
		t.Fatal(err)
	}
	if review.Message != "Nice site\nthanks" || review.Rating == nil || *review.Rating != 4 {
		t.Fatalf("review: %+v", review)
	}
	// A rating only makes sense on a review.
	bug, err := f.svc.SendFeedback(bob, service.FeedbackInput{Kind: store.FeedbackBug, Rating: ptr(int64(1)), Message: "Schedule is empty"})
	if err != nil || bug.Rating != nil {
		t.Fatalf("bug: %v %+v", err, bug)
	}

	for name, in := range map[string]service.FeedbackInput{
		"kind":     {Kind: "rant", Message: "x"},
		"rating":   {Kind: store.FeedbackReview, Rating: ptr(int64(6)), Message: "x"},
		"empty":    {Kind: store.FeedbackIdea, Message: "  \n "},
		"too long": {Kind: store.FeedbackIdea, Message: strings.Repeat("я", 4001)},
	} {
		var ie *service.InputError
		if _, err := f.svc.SendFeedback(alice, in); !errors.As(err, &ie) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := f.svc.SendFeedback(context.Background(), service.FeedbackInput{Kind: store.FeedbackIdea, Message: "x"}); !errors.Is(err, service.ErrUnauthenticated) {
		t.Errorf("anonymous: %v", err)
	}

	// Users see only their own.
	mine, err := f.svc.MyFeedback(alice)
	if err != nil || len(mine) != 1 || mine[0].ID != review.ID {
		t.Fatalf("alice's feedback: %v %+v", err, mine)
	}

	// Only superadmins read the inbox and act on it.
	if _, err := f.svc.Inbox(alice, true); !errors.Is(err, service.ErrForbidden) {
		t.Errorf("student inbox: %v", err)
	}
	if err := f.svc.ResolveFeedback(bob, bug.ID, true); !errors.Is(err, service.ErrForbidden) {
		t.Errorf("student resolve: %v", err)
	}
	if err := f.svc.ResolveFeedback(root, bug.ID, true); err != nil {
		t.Fatal(err)
	}
	inbox, err := f.svc.Inbox(root, true)
	if err != nil || inbox.Open != 1 || len(inbox.Items) != 1 || inbox.Items[0].ID != review.ID || inbox.Items[0].Username != "alice" {
		t.Fatalf("open inbox: %v %+v", err, inbox)
	}
	if inbox, _ = f.svc.Inbox(root, false); len(inbox.Items) != 2 || inbox.Items[0].ResolvedAt == nil {
		t.Fatalf("whole inbox, newest (resolved bug) first: %+v", inbox.Items)
	}
	if err := f.svc.DeleteFeedback(root, review.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.DeleteFeedback(root, review.ID); !errors.Is(err, service.ErrNotFound) {
		t.Errorf("deleting twice: %v", err)
	}
}

func TestFeedbackRateLimit(t *testing.T) {
	f := setup(t)
	alice := f.as(t, f.register(t, "alice"))
	var err error
	for i := 0; i < 11 && err == nil; i++ {
		_, err = f.svc.SendFeedback(alice, service.FeedbackInput{Kind: store.FeedbackIdea, Message: "idea"})
	}
	if !errors.Is(err, service.ErrRateLimited) {
		t.Fatalf("11th feedback in an hour: %v", err)
	}
}
