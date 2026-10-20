package service

import (
	"context"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/sqprrr/ExtendedTimetable/internal/auth"
	"github.com/sqprrr/ExtendedTimetable/internal/cist"
	"github.com/sqprrr/ExtendedTimetable/internal/i18n"
	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

// Anyone, signed in or not, can ask for the site for their group: those who
// ask are mostly group leaders without an account yet. Superadmins read the
// requests and get in touch on Telegram.

// cistUniversity is the university whose groups are looked up in CIST.
const cistUniversity = "ХНУРЕ"

// Universities are the universities the request form offers, the first by
// default; UniversityOther lets the visitor type another one.
var Universities = []string{cistUniversity}

// UniversityOther is the form's choice for a university not in Universities.
const UniversityOther = "other"

const (
	maxUniversityLen = 200
	maxGroupNameLen  = 50
	maxCommentLen    = 2000
	// cistLookupTimeout bounds the CIST lookup, which the visitor waits for.
	cistLookupTimeout = 5 * time.Second
)

// GroupRequestInput is the group request form.
type GroupRequestInput struct {
	// University is one of Universities, or UniversityOther with the name
	// in OtherUniversity.
	University      string
	OtherUniversity string
	Group           string
	// Telegram is a username, with or without '@', or a t.me link to it.
	Telegram string
	Comment  string
	ClientIP string
}

// telegramRe is a Telegram username: 5–32 latin letters, digits and '_',
// starting with a letter (a few older or bought ones are 4 long).
var telegramRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{3,31}$`)

// telegramUsername returns the username in s ("@name", "name" or a t.me
// link to it) without the '@'; ok is false when s holds none.
func telegramUsername(s string) (name string, ok bool) {
	s = strings.TrimSpace(s)
	// In this order, so that "https://www.t.me/name" loses all three.
	for _, prefix := range []string{"https://", "http://", "www.", "t.me/", "telegram.me/"} {
		if len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix) {
			s = s[len(prefix):]
		}
	}
	s = strings.TrimSuffix(strings.TrimPrefix(s, "@"), "/")
	return s, telegramRe.MatchString(s)
}

func (in GroupRequestInput) validate() (GroupRequestInput, error) {
	var err error
	switch {
	case in.University == UniversityOther:
		if in.University, err = text("other_university", "field.university", in.OtherUniversity, true, maxUniversityLen); err != nil {
			return in, err
		}
	case !slices.Contains(Universities, in.University):
		return in, inputError("university", "err.university")
	}
	if in.Group, err = text("group", "field.group", in.Group, true, maxGroupNameLen); err != nil {
		return in, err
	}
	var ok bool
	if in.Telegram, ok = telegramUsername(in.Telegram); !ok {
		return in, inputError("telegram", "err.telegram")
	}
	in.Comment = strings.TrimSpace(strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(in.Comment))
	// Superadmins also read requests in a terminal (extt admin requests),
	// where control characters are escape sequences.
	if strings.ContainsFunc(in.Comment, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\t' }) {
		return in, inputError("comment", "err.control_chars", "Field", i18n.M("field.comment"))
	}
	if utf8.RuneCountInString(in.Comment) > maxCommentLen {
		return in, inputError("comment", "err.too_long", "Field", i18n.M("field.comment"), "Count", maxCommentLen)
	}
	return in, nil
}

// RequestGroup stores a request for the site for a group, with the CIST
// group of that name when the university is in CIST and it finds one.
func (s *Service) RequestGroup(ctx context.Context, in GroupRequestInput) (*store.GroupRequest, error) {
	in, err := in.validate()
	if err != nil {
		return nil, err
	}
	if !s.groupRequestByIP.Allow(auth.ClientNetwork(in.ClientIP)) || !s.groupRequestAll.Allow("") {
		slog.WarnContext(ctx, "group request rate limited")
		return nil, ErrRateLimited
	}
	gr := &store.GroupRequest{University: in.University, GroupName: in.Group, Telegram: in.Telegram,
		Comment: in.Comment, CreatedAt: s.now()}
	if in.University == cistUniversity {
		if g := s.findCISTGroup(ctx, in.Group); g != nil {
			gr.CISTID, gr.CISTName = &g.ID, g.Name
		}
	}
	if err := s.store.CreateGroupRequest(ctx, gr); err != nil {
		return nil, err
	}
	return gr, nil
}

// findCISTGroup returns the CIST group named name, ignoring case and spaces.
// A group whose name only contains it is not taken: "КІУКІ-25" may well be
// another group than the one asked for. nil when there is none or CIST does
// not answer in time: the request is kept all the same, and a superadmin
// looks the group up by hand.
func (s *Service) findCISTGroup(ctx context.Context, name string) *cist.Group {
	if s.cfg.CISTGroups == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, cistLookupTimeout)
	defer cancel()
	groups, err := s.cfg.CISTGroups.FindGroups(ctx, noSpaces(name))
	if err != nil {
		slog.WarnContext(ctx, "CIST group lookup failed", "err", err)
		return nil
	}
	for _, g := range groups {
		if strings.EqualFold(noSpaces(g.Name), noSpaces(name)) {
			return &g
		}
	}
	return nil
}

// noSpaces is s without its white space: "КІУКІ 25-3" is "КІУКІ25-3".
func noSpaces(s string) string {
	return strings.Join(strings.Fields(s), "")
}

// GroupRequestInbox is the superadmins' view of the group requests.
type GroupRequestInbox struct {
	Items []*store.GroupRequest
	// Open counts the unresolved requests, whether Items holds all or not.
	Open int
}

// GroupRequests lists the group requests, newest first: all of them, or
// only the unresolved. Superadmins only.
func (s *Service) GroupRequests(ctx context.Context, openOnly bool) (*GroupRequestInbox, error) {
	if _, err := requireSuperadmin(ctx); err != nil {
		return nil, err
	}
	return s.groupRequests(ctx, openOnly)
}

func (s *Service) groupRequests(ctx context.Context, openOnly bool) (*GroupRequestInbox, error) {
	items, err := s.store.ListGroupRequests(ctx, openOnly)
	if err != nil {
		return nil, err
	}
	open, err := s.store.CountOpenGroupRequests(ctx)
	if err != nil {
		return nil, err
	}
	return &GroupRequestInbox{Items: items, Open: open}, nil
}

// ResolveGroupRequest marks a group request resolved, or open again.
// Superadmins only.
func (s *Service) ResolveGroupRequest(ctx context.Context, id int64, resolved bool) error {
	if _, err := requireSuperadmin(ctx); err != nil {
		return err
	}
	var at *time.Time
	if resolved {
		now := s.now()
		at = &now
	}
	return notFound(s.store.SetGroupRequestResolved(ctx, id, at))
}

// DeleteGroupRequest removes a group request. Superadmins only.
func (s *Service) DeleteGroupRequest(ctx context.Context, id int64) error {
	if _, err := requireSuperadmin(ctx); err != nil {
		return err
	}
	return notFound(s.store.DeleteGroupRequest(ctx, id))
}

// AdminGroupRequests lists group requests for the `extt admin requests`
// command.
func (s *Service) AdminGroupRequests(ctx context.Context, openOnly bool) (*GroupRequestInbox, error) {
	return s.groupRequests(ctx, openOnly)
}

// GroupRequest returns a group request. Superadmins only.
func (s *Service) GroupRequest(ctx context.Context, id int64) (*store.GroupRequest, error) {
	if _, err := requireSuperadmin(ctx); err != nil {
		return nil, err
	}
	gr, err := s.store.GroupRequestByID(ctx, id)
	return gr, notFound(err)
}

// GroupFromRequest fills the new-group form in from a request: the name is
// CIST's name for the group if the form found it, else the one asked for,
// and the code is that name in latin letters.
func GroupFromRequest(gr *store.GroupRequest) CreateGroupInput {
	name := gr.GroupName
	if gr.CISTName != "" {
		name = gr.CISTName
	}
	return CreateGroupInput{Code: groupCode(name), Name: name, CISTGroupID: gr.CISTID, RequestID: &gr.ID}
}

// latin spells Ukrainian letters in latin ones, as the official
// transliteration does at the start of a word: КІУКІ-25-3 is KIUKI-25-3.
var latin = map[rune]string{
	'А': "A", 'Б': "B", 'В': "V", 'Г': "H", 'Ґ': "G", 'Д': "D", 'Е': "E", 'Є': "YE", 'Ж': "ZH",
	'З': "Z", 'И': "Y", 'І': "I", 'Ї': "YI", 'Й': "Y", 'К': "K", 'Л': "L", 'М': "M", 'Н': "N",
	'О': "O", 'П': "P", 'Р': "R", 'С': "S", 'Т': "T", 'У': "U", 'Ф': "F", 'Х': "KH", 'Ц': "TS",
	'Ч': "CH", 'Ш': "SH", 'Щ': "SHCH", 'Ь': "", 'Ю': "YU", 'Я': "YA", '\'': "", '’': "",
}

// groupCode turns a group's name into a group code: latin capitals, digits,
// '-' and '_', at most 32 long. Spaces become '-'; anything else is left out.
func groupCode(name string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(strings.TrimSpace(name)) {
		switch {
		case r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_':
			b.WriteRune(r)
		case unicode.IsSpace(r):
			b.WriteByte('-')
		default:
			b.WriteString(latin[r])
		}
	}
	code := b.String()
	if len(code) > 32 {
		code = code[:32]
	}
	return code
}
