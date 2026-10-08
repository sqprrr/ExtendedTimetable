package service_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/sqprrr/ExtendedTimetable/internal/service"
	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

func role(t *testing.T, f *fixture, sess *service.NewSession) store.Role {
	t.Helper()
	r, _ := service.ViewerFrom(f.as(t, sess)).RoleIn(f.group.ID)
	return r
}

func logEvents(t *testing.T, f *fixture) []store.LogEvent {
	t.Helper()
	entries, err := f.svc.AdminGroupLog(context.Background(), f.group.Code, 100)
	if err != nil {
		t.Fatal(err)
	}
	var out []store.LogEvent
	for i := len(entries) - 1; i >= 0; i-- {
		out = append(out, entries[i].Event)
	}
	return out
}

func TestInviteExpiresAndIsReplaced(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	old := f.invite(t, f.group.Code)
	if inv, err := f.svc.Invite(ctx, old); err != nil || inv.Group.ID != f.group.ID {
		t.Fatalf("fresh invite: %+v %v", inv, err)
	}

	lead := f.leader(t, "lead")
	inv, err := f.svc.RegenerateInvite(lead, f.group.ID)
	if err != nil {
		t.Fatal(err)
	}
	if inv.Token == old {
		t.Fatal("regenerating should change the token")
	}
	if _, err := f.svc.Invite(ctx, old); !errors.Is(err, service.ErrInviteInvalid) {
		t.Fatalf("replaced invite: got %v, want ErrInviteInvalid", err)
	}
	// Members who joined through the old link stay.
	if _, ok := service.ViewerFrom(lead).RoleIn(f.group.ID); !ok {
		t.Fatal("regenerating removed a member")
	}

	service.SetClock(f.svc, func() time.Time { return time.Now().Add(service.InviteTTL + time.Minute) })
	if _, err := f.svc.Invite(ctx, inv.Token); !errors.Is(err, service.ErrInviteInvalid) {
		t.Fatalf("expired invite: got %v, want ErrInviteInvalid", err)
	}
	_, err = f.svc.Register(ctx, service.RegisterInput{Username: "late", Password: "correct horse", InviteToken: inv.Token, ClientIP: "l"})
	if !errors.Is(err, service.ErrInviteInvalid) {
		t.Fatalf("register with expired invite: got %v", err)
	}
}

func TestOnlyLeaderAndSuperadminSeeTheInvite(t *testing.T) {
	f := setup(t)
	stud := f.as(t, f.register(t, "stud"))
	if _, err := f.svc.GroupInvite(stud, f.group.ID); !errors.Is(err, service.ErrForbidden) {
		t.Fatalf("student sees invite: %v", err)
	}
	if _, err := f.svc.RegenerateInvite(stud, f.group.ID); !errors.Is(err, service.ErrForbidden) {
		t.Fatalf("student regenerates invite: %v", err)
	}
	if _, err := f.svc.GroupInvite(f.leader(t, "lead"), f.group.ID); err != nil {
		t.Fatalf("leader: %v", err)
	}
	if _, err := f.svc.GroupInvite(f.superadmin(t), f.group.ID); err != nil {
		t.Fatalf("superadmin: %v", err)
	}
}

func TestJoinGroupWithAnAccount(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	if _, err := f.svc.AdminCreateGroup(ctx, "OTHER-1", "Other group", nil); err != nil {
		t.Fatal(err)
	}
	sess := f.register(t, "alice")
	alice := f.as(t, sess)
	token := f.invite(t, f.group.Code)

	// Joining one's own group again does nothing.
	if g, err := f.svc.JoinGroup(alice, token); err != nil || g.ID != f.group.ID {
		t.Fatalf("rejoin: %v %v", g, err)
	}
	inv, err := f.svc.Invite(alice, token)
	if err != nil || !inv.Member || inv.OtherGroup != nil {
		t.Fatalf("invite view for a member: %+v %v", inv, err)
	}

	// A user is in one group at most.
	other := f.invite(t, "OTHER-1")
	inv, err = f.svc.Invite(alice, other)
	if err != nil || inv.Member || inv.OtherGroup == nil || inv.OtherGroup.ID != f.group.ID {
		t.Fatalf("invite view for a member of another group: %+v %v", inv, err)
	}
	if _, err := f.svc.JoinGroup(alice, other); err == nil {
		t.Fatal("joined a second group")
	} else {
		wantInputError(t, err, "group")
	}

	// After leaving, the link of another group works.
	if err := f.svc.LeaveGroup(alice, f.group.ID); err != nil {
		t.Fatal(err)
	}
	alice = f.as(t, sess)
	if len(service.ViewerFrom(alice).Memberships) != 0 {
		t.Fatal("still a member after leaving")
	}
	g, err := f.svc.JoinGroup(alice, other)
	if err != nil || g.Code != "OTHER-1" {
		t.Fatalf("join after leaving: %v %v", g, err)
	}
	if _, err := f.svc.JoinGroup(alice, "nope"); !errors.Is(err, service.ErrInviteInvalid) {
		t.Fatalf("bad token: %v", err)
	}
}

func TestClaimAndResignLeadership(t *testing.T) {
	f := setup(t)
	annSess, bobSess := f.register(t, "ann"), f.register(t, "bob")
	ann, bob := f.as(t, annSess), f.as(t, bobSess)

	if err := f.svc.ClaimLeadership(ann, f.group.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.ClaimLeadership(bob, f.group.ID); !errors.Is(err, service.ErrLeaderTaken) {
		t.Fatalf("second claim: got %v, want ErrLeaderTaken", err)
	}
	if role(t, f, annSess) != store.RoleLeader || role(t, f, bobSess) != store.RoleStudent {
		t.Fatal("ann should be the only leader")
	}
	g, err := f.svc.Group(f.as(t, bobSess), f.group.Code)
	if err != nil || g.Leader != "ann" || g.Role != store.RoleStudent {
		t.Fatalf("group view: %+v %v", g, err)
	}

	// The leader cannot leave before giving up the role.
	ann = f.as(t, annSess)
	if err := f.svc.LeaveGroup(ann, f.group.ID); !errors.Is(err, service.ErrLeaderMustResign) {
		t.Fatalf("leader leaving: got %v", err)
	}
	if err := f.svc.ResignLeadership(bob, f.group.ID); !errors.Is(err, service.ErrForbidden) {
		t.Fatalf("student resigning: got %v", err)
	}
	if err := f.svc.ResignLeadership(ann, f.group.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.ClaimLeadership(bob, f.group.ID); err != nil {
		t.Fatalf("claim after resign: %v", err)
	}

	// A non-member cannot claim.
	if _, err := f.svc.AdminCreateGroup(context.Background(), "OTHER-1", "", nil); err != nil {
		t.Fatal(err)
	}
	outsider := f.as(t, f.registerInto(t, "OTHER-1", "olga"))
	if err := f.svc.ClaimLeadership(outsider, f.group.ID); !errors.Is(err, service.ErrForbidden) {
		t.Fatalf("outsider claim: got %v", err)
	}

	want := []store.LogEvent{store.LogGroupCreated, store.LogJoined, store.LogJoined,
		store.LogLeaderClaimed, store.LogLeaderResigned, store.LogLeaderClaimed}
	if got := logEvents(t, f); !slices.Equal(got, want) {
		t.Fatalf("log: %v\nwant %v", got, want)
	}
}

func TestRemoveMember(t *testing.T) {
	f := setup(t)
	leadSess := f.register(t, "lead")
	if err := f.svc.AdminSetLeader(context.Background(), "lead", f.group.Code); err != nil {
		t.Fatal(err)
	}
	lead := f.as(t, leadSess)
	studSess := f.register(t, "stud")
	stud := f.as(t, studSess)
	members, err := f.svc.Members(stud, f.group.ID)
	if err != nil || len(members) != 2 || members[0].Username != "lead" {
		t.Fatalf("members: %v %v", members, err)
	}
	studID, leadID := members[1].UserID, members[0].UserID

	if err := f.svc.RemoveMember(stud, f.group.ID, leadID); !errors.Is(err, service.ErrForbidden) {
		t.Fatalf("student removes: %v", err)
	}
	if err := f.svc.RemoveMember(lead, f.group.ID, leadID); !errors.Is(err, service.ErrForbidden) {
		t.Fatalf("leader removes themselves: %v", err)
	}

	old := f.invite(t, f.group.Code)
	if err := f.svc.RemoveMember(lead, f.group.ID, studID); err != nil {
		t.Fatal(err)
	}
	if role(t, f, studSess) != "" {
		t.Fatal("removed student is still a member")
	}
	// The link is replaced so they cannot come straight back.
	if _, err := f.svc.JoinGroup(f.as(t, studSess), old); !errors.Is(err, service.ErrInviteInvalid) {
		t.Fatalf("rejoin with the old link: %v", err)
	}
	if err := f.svc.RemoveMember(lead, f.group.ID, studID); !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("remove twice: %v", err)
	}

	// A superadmin may remove the leader.
	if err := f.svc.RemoveMember(f.superadmin(t), f.group.ID, leadID); err != nil {
		t.Fatal(err)
	}
	if role(t, f, leadSess) != "" {
		t.Fatal("removed leader is still a member")
	}
}

func TestSuperadminSetsAndRemovesTheLeader(t *testing.T) {
	f := setup(t)
	annSess, bobSess := f.register(t, "ann"), f.register(t, "bob")
	root := f.superadmin(t)
	members, err := f.svc.Members(root, f.group.ID)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]int64{}
	for _, m := range members {
		ids[m.Username] = m.UserID
	}

	if err := f.svc.SetLeader(f.as(t, annSess), f.group.ID, ids["ann"]); !errors.Is(err, service.ErrForbidden) {
		t.Fatalf("student sets leader: %v", err)
	}
	if err := f.svc.SetLeader(root, f.group.ID, ids["ann"]); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.SetLeader(root, f.group.ID, ids["bob"]); err != nil {
		t.Fatal(err)
	}
	if role(t, f, annSess) != store.RoleStudent || role(t, f, bobSess) != store.RoleLeader {
		t.Fatal("the role should have moved from ann to bob")
	}
	if err := f.svc.SetLeader(root, f.group.ID, 99999); !errors.Is(err, service.ErrNotFound) {
		t.Fatalf("non-member: %v", err)
	}
	if err := f.svc.RemoveLeader(root, f.group.ID); err != nil {
		t.Fatal(err)
	}
	if role(t, f, bobSess) != store.RoleStudent {
		t.Fatal("bob is still the leader")
	}
	// Without a leader, anyone may claim the role again.
	if err := f.svc.ClaimLeadership(f.as(t, annSess), f.group.ID); err != nil {
		t.Fatal(err)
	}

	entries, err := f.svc.GroupLog(root, f.group.ID)
	if err != nil {
		t.Fatal(err)
	}
	if e := entries[1]; e.Event != store.LogLeaderRemoved || e.ActorName != "root" || e.UserName != "bob" {
		t.Fatalf("log entry: %+v", e)
	}
	if _, err := f.svc.GroupLog(f.as(t, annSess), f.group.ID); !errors.Is(err, service.ErrForbidden) {
		t.Fatalf("leader reads the log: %v", err)
	}
}

func TestCreateGroupInPanel(t *testing.T) {
	f := setup(t)
	stud := f.as(t, f.register(t, "stud"))
	if _, err := f.svc.CreateGroup(stud, service.CreateGroupInput{Code: "NEW-1"}); !errors.Is(err, service.ErrForbidden) {
		t.Fatalf("student creates group: %v", err)
	}
	root := f.superadmin(t)
	cist := int64(42)
	g, err := f.svc.CreateGroup(root, service.CreateGroupInput{Code: " new-1 ", Name: "New", CISTGroupID: &cist})
	if err != nil || g.Code != "NEW-1" || *g.CISTGroupID != 42 {
		t.Fatalf("create: %+v %v", g, err)
	}
	_, err = f.svc.CreateGroup(root, service.CreateGroupInput{Code: "new-1"})
	wantInputError(t, err, "code")

	groups, err := f.svc.AdminGroups(root)
	if err != nil || len(groups) != 2 || groups[1].Code != "NEW-1" || groups[1].Invite == nil || groups[1].Members != 0 {
		t.Fatalf("admin groups: %+v %v", groups, err)
	}
}

func TestEditorEditsContentButNotMembers(t *testing.T) {
	f := setup(t)
	lead := f.leader(t, "lead")
	edSess, studSess := f.register(t, "eddie"), f.register(t, "stud")
	edID := service.ViewerFrom(f.as(t, edSess)).UserID
	studID := service.ViewerFrom(f.as(t, studSess)).UserID
	gid := f.group.ID

	// Only the leader and superadmins hand out the role.
	if err := f.svc.GrantEditor(f.as(t, studSess), gid, edID); !errors.Is(err, service.ErrForbidden) {
		t.Fatalf("student grants editor: %v", err)
	}
	if err := f.svc.GrantEditor(lead, gid, edID); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.GrantEditor(lead, gid, edID); err != nil {
		t.Fatalf("granting twice: %v", err)
	}
	if role(t, f, edSess) != store.RoleEditor {
		t.Fatal("ed should be an editor")
	}
	ed := f.as(t, edSess)
	if err := f.svc.GrantEditor(ed, gid, studID); !errors.Is(err, service.ErrForbidden) {
		t.Fatalf("editor grants editor: %v", err)
	}
	leadID := service.ViewerFrom(lead).UserID
	if err := f.svc.GrantEditor(lead, gid, leadID); !errors.Is(err, service.ErrForbidden) {
		t.Fatalf("granting editor to the leader: %v", err)
	}

	// The editor changes content like the leader.
	g, err := f.svc.Group(ed, f.group.Code)
	if err != nil || !g.CanEdit || g.CanManage || g.Role != store.RoleEditor {
		t.Fatalf("editor's group view: %+v %v", g, err)
	}
	sub, err := f.svc.CreateSubject(ed, gid, service.SubjectInput{Name: "Physics"})
	if err != nil {
		t.Fatalf("editor creates subject: %v", err)
	}
	if _, err := f.svc.CreateNote(ed, gid, service.NoteInput{Title: "Exam", Body: "On Friday"}); err != nil {
		t.Fatalf("editor creates note: %v", err)
	}
	if err := f.svc.DeleteSubject(ed, gid, sub.ID); err != nil {
		t.Fatalf("editor deletes subject: %v", err)
	}
	if _, err := f.svc.CreateSubject(f.as(t, studSess), gid, service.SubjectInput{Name: "Chemistry"}); !errors.Is(err, service.ErrForbidden) {
		t.Fatalf("student creates subject: %v", err)
	}

	// ...but not the invite link or members.
	if _, err := f.svc.GroupInvite(ed, gid); !errors.Is(err, service.ErrForbidden) {
		t.Fatalf("editor sees invite: %v", err)
	}
	if _, err := f.svc.RegenerateInvite(ed, gid); !errors.Is(err, service.ErrForbidden) {
		t.Fatalf("editor regenerates invite: %v", err)
	}
	if err := f.svc.RemoveMember(ed, gid, studID); !errors.Is(err, service.ErrForbidden) {
		t.Fatalf("editor removes member: %v", err)
	}
	if err := f.svc.RevokeEditor(ed, gid, edID); !errors.Is(err, service.ErrForbidden) {
		t.Fatalf("editor revokes editor: %v", err)
	}

	// Editors are listed after the leader.
	ms, err := f.svc.Members(ed, gid)
	if err != nil || len(ms) != 3 || ms[0].Username != "lead" || ms[1].Username != "eddie" {
		t.Fatalf("members order: %v %v", ms, err)
	}

	if err := f.svc.RevokeEditor(f.superadmin(t), gid, edID); err != nil {
		t.Fatal(err)
	}
	if role(t, f, edSess) != store.RoleStudent {
		t.Fatal("ed should be a student again")
	}
	if _, err := f.svc.CreateSubject(f.as(t, edSess), gid, service.SubjectInput{Name: "Biology"}); !errors.Is(err, service.ErrForbidden) {
		t.Fatalf("former editor creates subject: %v", err)
	}

	want := []store.LogEvent{store.LogGroupCreated, store.LogJoined, store.LogLeaderAssigned,
		store.LogJoined, store.LogJoined, store.LogEditorGranted, store.LogEditorRevoked}
	if got := logEvents(t, f); !slices.Equal(got, want) {
		t.Fatalf("log: %v\nwant %v", got, want)
	}
}

func TestEditorClaimsLeadership(t *testing.T) {
	f := setup(t)
	edSess := f.register(t, "eddie")
	root := f.superadmin(t)
	if err := f.svc.GrantEditor(root, f.group.ID, service.ViewerFrom(f.as(t, edSess)).UserID); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.ClaimLeadership(f.as(t, edSess), f.group.ID); err != nil {
		t.Fatal(err)
	}
	if role(t, f, edSess) != store.RoleLeader {
		t.Fatal("the editor should be the leader")
	}
}
