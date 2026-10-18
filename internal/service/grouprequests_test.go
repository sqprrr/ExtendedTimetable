package service_test

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/sqprrr/ExtendedTimetable/internal/cist"
	"github.com/sqprrr/ExtendedTimetable/internal/service"
)

// fakeGroups is CIST's list of groups.
type fakeGroups struct {
	groups []cist.Group
	err    error
	asked  []string
}

func (f *fakeGroups) FindGroups(_ context.Context, query string) ([]cist.Group, error) {
	f.asked = append(f.asked, query)
	if f.err != nil {
		return nil, f.err
	}
	var out []cist.Group
	for _, g := range f.groups {
		if strings.Contains(strings.ToLower(g.Name), strings.ToLower(query)) {
			out = append(out, g)
		}
	}
	return out, nil
}

func TestTelegramUsername(t *testing.T) {
	for in, want := range map[string]string{
		"@kirill_dev":                   "kirill_dev",
		"  kirill_dev ":                 "kirill_dev",
		"https://t.me/kirill_dev":       "kirill_dev",
		"HTTPS://WWW.T.ME/kirill_dev/":  "kirill_dev",
		"t.me/@kirill_dev":              "kirill_dev",
		"http://telegram.me/kirill_dev": "kirill_dev",
	} {
		if got, ok := service.TelegramUsername(in); !ok || got != want {
			t.Errorf("%q: %q %v, want %q", in, got, ok, want)
		}
	}
	for _, bad := range []string{"", "@", "abc", "1kirill", "kirill dev", "kirill-dev", "+380501234567",
		"https://example.org/kirill_dev", strings.Repeat("a", 33)} {
		if got, ok := service.TelegramUsername(bad); ok {
			t.Errorf("%q should be refused, got %q", bad, got)
		}
	}
}

func TestRequestGroup(t *testing.T) {
	groups := &fakeGroups{groups: []cist.Group{
		{ID: 11881842, Name: "КІУКІ-25-3"}, {ID: 11881843, Name: "КІУКІ-25-31"}, {ID: 7, Name: "ПЗПІ-25-1"},
	}}
	f := setupWith(t, service.Config{CISTGroups: groups})
	ctx := context.Background()
	ip := 0
	request := func(in service.GroupRequestInput) (*int64, string, error) {
		t.Helper()
		ip++
		in.ClientIP = "10.0.0." + strconv.Itoa(ip)
		gr, err := f.svc.RequestGroup(ctx, in)
		if err != nil {
			return nil, "", err
		}
		return gr.CISTID, gr.CISTName, nil
	}

	// The exact name wins over the longer one that contains it.
	id, name, err := request(service.GroupRequestInput{University: "ХНУРЕ", Group: " кіукі-25-3 ", Telegram: "@leader_25", Comment: " 28 людей\r\nдякую "})
	if err != nil || id == nil || *id != 11881842 || name != "КІУКІ-25-3" {
		t.Fatalf("exact match: %v %v %q", err, id, name)
	}
	// Only the exact name: a single group that merely contains it may be
	// another group.
	if id, _, err := request(service.GroupRequestInput{University: "ХНУРЕ", Group: "ПЗПІ-25", Telegram: "leader_25"}); err != nil || id != nil {
		t.Fatalf("partial name: %v %v", err, id)
	}
	if id, _, err := request(service.GroupRequestInput{University: "ХНУРЕ", Group: "пзпі-25-1", Telegram: "leader_25"}); err != nil || id == nil || *id != 7 {
		t.Fatalf("exact name in another case: %v %v", err, id)
	}
	// Several, none, or CIST down: the request is kept without a CIST group.
	if id, _, err := request(service.GroupRequestInput{University: "ХНУРЕ", Group: "КІУКІ-25", Telegram: "leader_25"}); err != nil || id != nil {
		t.Fatalf("several matches: %v %v", err, id)
	}
	groups.err = errors.New("cist: timed out")
	if id, _, err := request(service.GroupRequestInput{University: "ХНУРЕ", Group: "КІУКІ-25-3", Telegram: "leader_25"}); err != nil || id != nil {
		t.Fatalf("CIST down: %v %v", err, id)
	}
	groups.err = nil
	// Another university is not looked up in CIST.
	asked := len(groups.asked)
	if id, _, err := request(service.GroupRequestInput{University: service.UniversityOther, OtherUniversity: " КПІ ", Group: "КІУКІ-25-3", Telegram: "leader_25"}); err != nil || id != nil {
		t.Fatalf("other university: %v %v", err, id)
	}
	if len(groups.asked) != asked {
		t.Errorf("another university was looked up in CIST: %v", groups.asked)
	}

	for name, in := range map[string]service.GroupRequestInput{
		"no university":    {Group: "КІУКІ-25-3", Telegram: "leader_25"},
		"unknown":          {University: "КПІ", Group: "КІУКІ-25-3", Telegram: "leader_25"},
		"other, no name":   {University: service.UniversityOther, OtherUniversity: " ", Group: "КІУКІ-25-3", Telegram: "leader_25"},
		"no group":         {University: "ХНУРЕ", Group: " ", Telegram: "leader_25"},
		"long group":       {University: "ХНУРЕ", Group: strings.Repeat("я", 51), Telegram: "leader_25"},
		"bad telegram":     {University: "ХНУРЕ", Group: "КІУКІ-25-3", Telegram: "+380501234567"},
		"long comment":     {University: "ХНУРЕ", Group: "КІУКІ-25-3", Telegram: "leader_25", Comment: strings.Repeat("я", 2001)},
		"escape sequences": {University: "ХНУРЕ", Group: "КІУКІ-25-3", Telegram: "leader_25", Comment: "hi\x1b[2J"},
	} {
		var ie *service.InputError
		if _, _, err := request(in); !errors.As(err, &ie) {
			t.Errorf("%s: %v", name, err)
		}
	}

	root := f.superadmin(t)
	inbox, err := f.svc.GroupRequests(root, true)
	if err != nil || inbox.Open != 6 || len(inbox.Items) != 6 {
		t.Fatalf("inbox: %v %+v", err, inbox)
	}
	// Newest first; the form's text is kept as cleaned up.
	last, first := inbox.Items[0], inbox.Items[5]
	if last.University != "КПІ" || first.GroupName != "кіукі-25-3" || first.Telegram != "leader_25" || first.Comment != "28 людей\nдякую" {
		t.Fatalf("stored: %+v %+v", last, first)
	}
	if err := f.svc.ResolveGroupRequest(root, first.ID, true); err != nil {
		t.Fatal(err)
	}
	if inbox, _ = f.svc.GroupRequests(root, true); inbox.Open != 5 || len(inbox.Items) != 5 {
		t.Fatalf("after resolving: %+v", inbox)
	}
	if inbox, _ = f.svc.GroupRequests(root, false); len(inbox.Items) != 6 || inbox.Items[5].ResolvedAt == nil {
		t.Fatalf("all: %+v", inbox)
	}
	if err := f.svc.DeleteGroupRequest(root, first.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.DeleteGroupRequest(root, first.ID); !errors.Is(err, service.ErrNotFound) {
		t.Errorf("delete twice: %v", err)
	}

	// Only superadmins read them.
	student := f.as(t, f.register(t, "alice"))
	if _, err := f.svc.GroupRequests(student, true); !errors.Is(err, service.ErrForbidden) {
		t.Errorf("student: %v", err)
	}
	if err := f.svc.ResolveGroupRequest(student, last.ID, true); !errors.Is(err, service.ErrForbidden) {
		t.Errorf("student resolve: %v", err)
	}
	if _, err := f.svc.GroupRequests(ctx, true); !errors.Is(err, service.ErrUnauthenticated) {
		t.Errorf("anonymous: %v", err)
	}
}

func TestRequestGroupRateLimit(t *testing.T) {
	f := setup(t)
	in := service.GroupRequestInput{University: "ХНУРЕ", Group: "КІУКІ-25-3", Telegram: "leader_25", ClientIP: "10.0.0.1"}
	for i := range 5 {
		if _, err := f.svc.RequestGroup(context.Background(), in); err != nil {
			t.Fatalf("request %d: %v", i+1, err)
		}
	}
	if _, err := f.svc.RequestGroup(context.Background(), in); !errors.Is(err, service.ErrRateLimited) {
		t.Fatalf("sixth from one address: %v", err)
	}
	in.ClientIP = "10.0.0.2"
	if _, err := f.svc.RequestGroup(context.Background(), in); err != nil {
		t.Fatalf("another address: %v", err)
	}
}

func TestGroupCode(t *testing.T) {
	for in, want := range map[string]string{
		"КІУКІ-25-3":     "KIUKI-25-3",
		" пзпі-24-1 ":    "PZPI-24-1",
		"ІТШІ 23 2":      "ITSHI-23-2",
		"Є-Ї-Ю-Я-Щ":      "YE-YI-YU-YA-SHCH",
		"ІП-21":          "IP-21",
		"KIUKI-25-3":     "KIUKI-25-3",
		"Об’єднана (№1)": "OBYEDNANA-1",
	} {
		if got := service.GroupCode(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
	if got := service.GroupCode(strings.Repeat("Щ", 20)); len(got) != 32 {
		t.Errorf("long name: %q", got)
	}
}

func TestCreateGroupFromRequest(t *testing.T) {
	f := setupWith(t, service.Config{CISTGroups: &fakeGroups{groups: []cist.Group{{ID: 11881844, Name: "КІУКІ-25-4"}}}})
	gr, err := f.svc.RequestGroup(context.Background(), service.GroupRequestInput{
		University: "ХНУРЕ", Group: "кіукі-25-4", Telegram: "leader_25", ClientIP: "10.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	in := service.GroupFromRequest(gr)
	if in.Code != "KIUKI-25-4" || in.Name != "КІУКІ-25-4" || in.CISTGroupID == nil || *in.CISTGroupID != 11881844 || *in.RequestID != gr.ID {
		t.Fatalf("form: %+v", in)
	}
	root := f.superadmin(t)
	if _, err := f.svc.CreateGroup(root, in); err != nil {
		t.Fatal(err)
	}
	if got, err := f.svc.GroupRequest(root, gr.ID); err != nil || got.ResolvedAt == nil {
		t.Fatalf("the request should be resolved: %v %+v", err, got)
	}
	// A request deleted meanwhile is no reason not to make the group.
	missing := int64(999)
	if _, err := f.svc.CreateGroup(root, service.CreateGroupInput{Code: "OTHER", RequestID: &missing}); err != nil {
		t.Fatalf("missing request: %v", err)
	}
}

// Spaces typed into the group's name do not stop the CIST lookup.
func TestRequestGroupIgnoresSpaces(t *testing.T) {
	f := setupWith(t, service.Config{CISTGroups: &fakeGroups{groups: []cist.Group{{ID: 11881842, Name: "КІУКІ-25-3"}}}})
	gr, err := f.svc.RequestGroup(context.Background(), service.GroupRequestInput{
		University: "ХНУРЕ", Group: "кіукі- 25 -3", Telegram: "leader_25", ClientIP: "10.0.0.1"})
	if err != nil || gr.CISTID == nil || *gr.CISTID != 11881842 {
		t.Fatalf("lookup with spaces: %v %+v", err, gr)
	}
}
