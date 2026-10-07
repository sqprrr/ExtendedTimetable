package service

import (
	"context"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sqprrr/ExtendedTimetable/internal/i18n"
	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

// Any signed-in user can send feedback (a bug report, a suggestion or a
// review) to the site's superadmins and see what they have sent. Only
// superadmins read everyone's, in the inbox.

// FeedbackKinds lists the kinds of feedback in the order the form offers them.
var FeedbackKinds = []store.FeedbackKind{store.FeedbackBug, store.FeedbackIdea, store.FeedbackReview}

const maxFeedbackLen = 4000

// FeedbackInput is the feedback form.
type FeedbackInput struct {
	Kind store.FeedbackKind
	// Rating is 1–5 or nil; it is kept for reviews only.
	Rating  *int64
	Message string
}

func (in FeedbackInput) validate() (FeedbackInput, error) {
	known := false
	for _, k := range FeedbackKinds {
		known = known || in.Kind == k
	}
	if !known {
		return in, inputError("kind", "err.feedback_kind")
	}
	if in.Kind != store.FeedbackReview {
		in.Rating = nil
	}
	if in.Rating != nil && (*in.Rating < 1 || *in.Rating > 5) {
		return in, inputError("rating", "err.rating")
	}
	in.Message = strings.TrimSpace(strings.ReplaceAll(in.Message, "\r\n", "\n"))
	if in.Message == "" {
		return in, inputError("message", "err.required", "Field", i18n.M("field.message"))
	}
	if utf8.RuneCountInString(in.Message) > maxFeedbackLen {
		return in, inputError("message", "err.too_long", "Field", i18n.M("field.message"), "Count", maxFeedbackLen)
	}
	return in, nil
}

// SendFeedback stores the viewer's feedback for the superadmins.
func (s *Service) SendFeedback(ctx context.Context, in FeedbackInput) (*store.Feedback, error) {
	v, err := requireViewer(ctx)
	if err != nil {
		return nil, err
	}
	if in, err = in.validate(); err != nil {
		return nil, err
	}
	if !s.feedbackByUser.Allow(strconv.FormatInt(v.UserID, 10)) {
		return nil, ErrRateLimited
	}
	f := &store.Feedback{UserID: &v.UserID, Kind: in.Kind, Rating: in.Rating, Message: in.Message, CreatedAt: s.now()}
	if err := s.store.CreateFeedback(ctx, f); err != nil {
		return nil, err
	}
	f.Username = v.Username
	return f, nil
}

// MyFeedback lists what the viewer has sent, newest first.
func (s *Service) MyFeedback(ctx context.Context) ([]*store.Feedback, error) {
	v, err := requireViewer(ctx)
	if err != nil {
		return nil, err
	}
	return s.store.FeedbackByUser(ctx, v.UserID)
}

// FeedbackInbox is the superadmins' view of the feedback.
type FeedbackInbox struct {
	Items []*store.Feedback
	// Open counts the unresolved feedback, whether Items holds all or not.
	Open int
}

// Inbox lists everyone's feedback, newest first: all of it, or only the
// unresolved. Superadmins only.
func (s *Service) Inbox(ctx context.Context, openOnly bool) (*FeedbackInbox, error) {
	if _, err := requireSuperadmin(ctx); err != nil {
		return nil, err
	}
	return s.inbox(ctx, openOnly)
}

func (s *Service) inbox(ctx context.Context, openOnly bool) (*FeedbackInbox, error) {
	items, err := s.store.ListFeedback(ctx, openOnly)
	if err != nil {
		return nil, err
	}
	open, err := s.store.CountOpenFeedback(ctx)
	if err != nil {
		return nil, err
	}
	return &FeedbackInbox{Items: items, Open: open}, nil
}

// OpenFeedbackCount counts the unresolved feedback. Superadmins only.
func (s *Service) OpenFeedbackCount(ctx context.Context) (int, error) {
	if _, err := requireSuperadmin(ctx); err != nil {
		return 0, err
	}
	return s.store.CountOpenFeedback(ctx)
}

// ResolveFeedback marks feedback resolved, or open again. Superadmins only.
func (s *Service) ResolveFeedback(ctx context.Context, id int64, resolved bool) error {
	if _, err := requireSuperadmin(ctx); err != nil {
		return err
	}
	var at *time.Time
	if resolved {
		now := s.now()
		at = &now
	}
	return notFound(s.store.SetFeedbackResolved(ctx, id, at))
}

// DeleteFeedback removes feedback. Superadmins only.
func (s *Service) DeleteFeedback(ctx context.Context, id int64) error {
	if _, err := requireSuperadmin(ctx); err != nil {
		return err
	}
	return notFound(s.store.DeleteFeedback(ctx, id))
}

// AdminFeedback lists feedback for the `extt admin feedback` command.
func (s *Service) AdminFeedback(ctx context.Context, openOnly bool) (*FeedbackInbox, error) {
	return s.inbox(ctx, openOnly)
}

// requireSuperadmin checks that the viewer is a superadmin.
func requireSuperadmin(ctx context.Context) (*Viewer, error) {
	v, err := requireViewer(ctx)
	if err != nil {
		return nil, err
	}
	if !v.IsSuperadmin {
		return nil, ErrForbidden
	}
	return v, nil
}
