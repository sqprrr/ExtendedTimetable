package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/sqprrr/ExtendedTimetable/internal/auth"
	"github.com/sqprrr/ExtendedTimetable/internal/store"
)

// The demo is a group that group leaders try the site in; Config.DemoGroup
// turns it on. It has two accounts: DemoUsername, its leader, to try what a
// leader does, and DemoStudentUsername, a student, to see (and show the
// group) what students see. Their passwords are random, made with the
// accounts and printed by `extt admin demo-reset`: the superadmin hands them
// to the leaders who ask, and nothing on the site shows them. ResetDemo
// fills the group with made-up homework, notes, class links and recordings,
// dated around the day it runs. The server runs it on start and every night,
// so whatever visitors did is gone by the morning. The demo accounts cannot
// give up the leader role, leave the group, join another or be removed
// (the leader may make the student an editor until the night); nobody joins
// the demo group, and its leader is shown no invite link; and the accounts'
// language, theme and time zone stay in each visitor's browser rather than
// in the account they all share. Only a group with nobody but the demo
// accounts in it can be the demo: a reset wipes the group's content.

const (
	DemoUsername        = "demo"
	DemoStudentUsername = "demo-student"
	// demoResetHour is the hour, in the site's time zone, of the nightly reset.
	demoResetHour = 4
	// demoPasswordLen is the length of a demo account's random password.
	demoPasswordLen = 12
)

// DemoAccounts are the demo's accounts, the leader first.
var DemoAccounts = []string{DemoUsername, DemoStudentUsername}

// demoRoles are the demo accounts' roles after a reset.
var demoRoles = map[string]store.Role{DemoUsername: store.RoleLeader, DemoStudentUsername: store.RoleStudent}

// ErrDemo is returned for what the demo accounts may not do.
var ErrDemo = errors.New("not available in the demo")

// ErrNotDemoGroup is returned by ResetDemo for a group with real members.
var ErrNotDemoGroup = errors.New("not a demo group")

// isDemoUser reports whether username is one of the demo accounts' names on
// this site. Registering under them is refused; whether an account bearing
// one is the demo account is isDemoMember.
func (s *Service) isDemoUser(username string) bool {
	return s.cfg.DemoGroup != "" && slices.Contains(DemoAccounts, username)
}

// isDemoMember reports whether the account with username and memberships is
// one of the demo accounts: a demo name in the demo group. A real account
// that got the name before the demo existed, in another group, is not, and
// keeps everything it may do.
func (s *Service) isDemoMember(ctx context.Context, username string, ms []store.Membership) bool {
	if !s.isDemoUser(username) {
		return false
	}
	id, ok := s.demoGroupID(ctx)
	return ok && slices.ContainsFunc(ms, func(m store.Membership) bool { return m.GroupID == id })
}

// isDemoAccount is isDemoMember for an account known by name only (at
// login); false when there is no such account.
func (s *Service) isDemoAccount(ctx context.Context, username string) bool {
	if !s.isDemoUser(username) {
		return false
	}
	u, err := s.store.UserByUsername(ctx, username)
	if err != nil {
		return false
	}
	ms, err := s.store.MembershipsByUser(ctx, u.ID)
	return err == nil && s.isDemoMember(ctx, username, ms)
}

// demoGroupID is the demo group's id; false when there is no demo group.
func (s *Service) demoGroupID(ctx context.Context) (int64, bool) {
	if s.cfg.DemoGroup == "" {
		return 0, false
	}
	g, err := s.groupByCode(ctx, s.cfg.DemoGroup)
	if err != nil {
		return 0, false
	}
	return g.ID, true
}

// demoSubjects are the subjects the demo creates when the group has fewer
// than two of its own (it has none without a CIST timetable).
var demoSubjects = []store.Subject{
	{Name: "Програмування", ShortName: "Прог"},
	{Name: "Вища математика", ShortName: "ВМ"},
	{Name: "Фізика", ShortName: "Фіз"},
	{Name: "Англійська мова", ShortName: "Англ"},
}

// demoHomework is an assignment of the demo. subject indexes the group's
// subjects (wrapping around); due is days from the reset (nil: no deadline).
type demoHomework struct {
	subject   int
	title     string
	desc      string
	due       *int
	maxPoints float64 // 0: not graded in points
	status    store.ProgressStatus
	grade     float64 // 0: none entered
}

func days(n int) *int { return &n }

var demoHomeworkList = []demoHomework{
	{0, "Лабораторна робота №2: масиви та цикли",
		"Реалізуйте функції пошуку мінімуму, максимуму та середнього значення в масиві.\n\n- код — у репозиторії на GitHub;\n- звіт — PDF на ДЛ.\n\nЗахист — на наступній лабораторній.",
		days(-4), 10, store.StatusDone, 9},
	{1, "Тест на ДЛ: модуль 1",
		"20 питань, 30 хвилин, одна спроба. Теми: границі, похідні, дослідження функцій.",
		days(1), 5, store.StatusInProgress, 0},
	{0, "Лабораторна робота №3: класи та об’єкти",
		"Опишіть клас **Student** з полями «ім’я», «група» та «оцінки». Додайте методи для середнього бала та виведення інформації.\n\nВаріант — за номером у списку групи.",
		days(3), 10, "", 0},
	{2, "Розрахункова робота: електростатика",
		"Розв’яжіть задачі 3.1–3.10 зі збірника (с. 45–48). Оформлення — від руки, скан у PDF.",
		days(6), 15, "", 0},
	{1, "Практичне заняття: задачі 4.1–4.12",
		"Підготуйте розв’язки до перевірки на парі. Задачі із зірочкою — за бажанням, +1 бал.",
		days(8), 0, "", 0},
	{3, "Essay: My future profession",
		"200–250 words. Use at least five words from Unit 4.\n\nShare it as a Google Doc with comments allowed.",
		days(12), 10, "", 0},
	{0, "Курсова робота: вибір теми",
		"Оберіть тему зі списку в оголошеннях і напишіть викладачу до кінця тижня.",
		nil, 0, "", 0},
}

var demoNotes = []store.Note{
	{Title: "Це демо-група", Pinned: true,
		BodyMD: "Тут усе вигадане: домашні завдання, оголошення, посилання й записи. Щоночі група повертається до початкового вигляду, тож можна сміливо все натискати.\n\nСподобалося? [Залиште заявку](/request), і ваша група отримає такий самий сайт."},
	{Title: "Консультація перед модулем",
		BodyMD: "У четвер о 16:00, ауд. 285. Приносьте питання з теми «Похідні»: розберемо типові помилки."},
	{Title: "Перенесення практики з фізики",
		BodyMD: "Практику з п’ятниці перенесено на понеділок, третя пара. Посилання на зустріч те саме."},
}

var demoClassLinks = []struct {
	subject int
	lesson  store.LessonType
	url     string
	note    string
}{
	{0, store.LessonLecture, "https://meet.google.com/abc-defg-hij", "Лекції, вхід з пошти @nure.ua"},
	{0, store.LessonLab, "https://meet.google.com/klm-nopq-rst", "Лабораторні, перша підгрупа"},
	{1, store.LessonLecture, "https://zoom.us/j/1234567890", "Пароль: 2025"},
	{2, store.LessonPractice, "https://meet.google.com/uvw-xyza-bcd", ""},
}

var demoResources = []struct {
	subject int
	kind    store.ResourceKind
	lesson  store.LessonType
	title   string
	url     string
	daysAgo int
}{
	{0, store.ResourceRecording, store.LessonLecture, "Лекція №1", "https://example.org/demo/programming-1", 14},
	{0, store.ResourceRecording, store.LessonLecture, "Лекція №2", "https://example.org/demo/programming-2", 7},
	{1, store.ResourceRecording, store.LessonLecture, "Лекція №1", "https://example.org/demo/math-1", 10},
	{0, store.ResourceSolution, store.LessonLab, "Розв’язки лабораторної №1", "https://example.org/demo/lab-1", 9},
}

// ResetDemo makes groupCode the demo group as it is every morning: the demo
// accounts are its leader and a student with the default settings, its
// timetable and subjects come again from CIST, and its content is the
// made-up set above. Whatever visitors changed (homework, notes, class
// links, recordings, playlists, subjects) is deleted. A demo account that
// does not exist yet is made with a random password; the new ones are
// returned by username (the others keep theirs). It refuses a demo account
// name that belongs to a superadmin or to a member of another group: that is
// someone's real account. It also refuses a group with other members: that is
// a real group (a mistyped EXTT_DEMO_GROUP), whose content it would wipe.
func (s *Service) ResetDemo(ctx context.Context, groupCode string) (passwords map[string]string, err error) {
	g, err := s.store.GroupByCode(ctx, strings.ToUpper(strings.TrimSpace(groupCode)))
	if err != nil {
		return nil, notFound(err)
	}
	members, err := s.store.ListMembers(ctx, g.ID)
	if err != nil {
		return nil, err
	}
	for _, m := range members {
		if !slices.Contains(DemoAccounts, m.Username) {
			return nil, fmt.Errorf("%w: %s has members other than the demo accounts (%s), so it is not the demo group", ErrNotDemoGroup, g.Code, m.Username)
		}
	}
	// Hashed out of the transaction, and only for new accounts: bcrypt is slow.
	passwords, hashes := map[string]string{}, map[string]string{}
	for _, name := range DemoAccounts {
		if _, err := s.store.UserByUsername(ctx, name); errors.Is(err, store.ErrNotFound) {
			passwords[name] = auth.NewToken()[:demoPasswordLen]
			if hashes[name], err = auth.HashPassword(passwords[name]); err != nil {
				return nil, err
			}
		} else if err != nil {
			return nil, err
		}
	}
	now := s.now()
	var userIDs []int64
	err = s.store.InTx(ctx, func(q *store.Queries) error {
		userIDs = userIDs[:0]
		for _, name := range DemoAccounts {
			u, err := s.demoAccount(ctx, q, g.ID, name, hashes[name], now)
			if err != nil {
				return err
			}
			userIDs = append(userIDs, u.ID)
		}
		return q.ClearGroupContent(ctx, g.ID)
	})
	if err != nil {
		return nil, err
	}
	// The timetable makes the group's subjects again. If CIST does not
	// answer, the demo's own subjects stand in, and the scheduled sync
	// brings the timetable later.
	if g.CISTGroupID != nil && s.cfg.CIST != nil {
		sctx, cancel := context.WithTimeout(ctx, firstSyncTimeout)
		s.syncDemoTimetable(sctx, g)
		cancel()
	}
	err = s.store.InTx(ctx, func(q *store.Queries) error {
		subjects, err := s.demoSubjects(ctx, q, g.ID)
		if err != nil {
			return err
		}
		return s.fillDemo(ctx, q, g.ID, userIDs, subjects, now)
	})
	if err != nil {
		return nil, err
	}
	slog.InfoContext(ctx, "demo group reset", "group", g.Code)
	return passwords, nil
}

// syncDemoTimetable syncs the demo group's timetable after a reset cleared
// it. A sync already running (the scheduled one, as the server starts) may
// have read or written before the reset, so this one waits for it and runs
// all the same. Failures are logged by syncGroup, or here.
func (s *Service) syncDemoTimetable(ctx context.Context, g *store.Group) {
	defer logPanic(ctx, "demo schedule sync")
	for {
		_, err := s.syncGroup(ctx, g, false)
		if !errors.Is(err, ErrSyncRunning) {
			var serr *SyncError
			if err != nil && !errors.As(err, &serr) && ctx.Err() == nil {
				slog.ErrorContext(ctx, "demo schedule sync", "group", g.Code, "err", err)
			}
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}

// demoAccount returns the demo account name as a member of the group in its
// demo role (demoRoles), with no settings of its own; for the leader,
// another leader becomes a student. An account that does not exist is made
// with the password hash (which is "" when the caller found one: then it is
// not used).
func (s *Service) demoAccount(ctx context.Context, q *store.Queries, groupID int64, name, hash string, now time.Time) (*store.User, error) {
	u, err := q.UserByUsername(ctx, name)
	if errors.Is(err, store.ErrNotFound) && hash != "" {
		u = &store.User{Username: name, PasswordHash: hash, CreatedAt: now}
		if err := q.CreateUser(ctx, u); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	if u.IsSuperadmin {
		return nil, fmt.Errorf("the account %q is a superadmin and cannot be a demo account", name)
	}
	ms, err := q.MembershipsByUser(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	member := false
	for _, m := range ms {
		if m.GroupID != groupID {
			return nil, fmt.Errorf("the account %q is in another group and cannot be a demo account", name)
		}
		member = true
	}
	role := demoRoles[name]
	if role == store.RoleLeader {
		if leader, err := q.LeaderOf(ctx, groupID); err == nil && leader.UserID != u.ID {
			if err := q.SetRole(ctx, leader.UserID, groupID, store.RoleStudent); err != nil {
				return nil, err
			}
		} else if err != nil && !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
	}
	if member {
		err = q.SetRole(ctx, u.ID, groupID, role)
	} else {
		err = q.AddMembership(ctx, &store.Membership{UserID: u.ID, GroupID: groupID, Role: role, JoinedAt: now})
	}
	if err != nil {
		return nil, err
	}
	for _, set := range []func() error{
		func() error { return q.SetLocale(ctx, u.ID, "") },
		func() error { return q.SetTheme(ctx, u.ID, "") },
		func() error { return q.SetTimeZone(ctx, u.ID, "") },
	} {
		if err := set(); err != nil {
			return nil, err
		}
	}
	return u, nil
}

// demoSubjects returns the group's subjects, the ones from CIST if it has
// them, or makes the demo's own.
func (s *Service) demoSubjects(ctx context.Context, q *store.Queries, groupID int64) ([]*store.Subject, error) {
	subjects, err := q.ListSubjects(ctx, groupID)
	if err != nil || len(subjects) >= 2 {
		return subjects, err
	}
	for _, d := range demoSubjects {
		exists := false
		for _, sub := range subjects {
			exists = exists || strings.EqualFold(sub.ShortName, d.ShortName)
		}
		if exists {
			continue
		}
		sub := &store.Subject{GroupID: groupID, Name: d.Name, ShortName: d.ShortName}
		if err := q.CreateSubject(ctx, sub); err != nil {
			return nil, err
		}
		subjects = append(subjects, sub)
	}
	return subjects, nil
}

// fillDemo adds the demo's content, with the same progress for each of the
// demo accounts.
func (s *Service) fillDemo(ctx context.Context, q *store.Queries, groupID int64, userIDs []int64, subjects []*store.Subject, now time.Time) error {
	subject := func(i int) int64 { return subjects[i%len(subjects)].ID }
	today := StartOfDay(now, s.cfg.Location)
	for _, d := range demoHomeworkList {
		hw := &store.Homework{GroupID: groupID, SubjectID: subject(d.subject), Title: d.title, DescriptionMD: d.desc,
			CreatedAt: now, UpdatedAt: now}
		if d.due != nil {
			due := today.AddDate(0, 0, *d.due).Add(23*time.Hour + 59*time.Minute)
			hw.DueAt = &due
		}
		if d.maxPoints > 0 {
			hw.MaxPoints = &d.maxPoints
		}
		if err := q.CreateHomework(ctx, hw); err != nil {
			return err
		}
		if d.status == "" {
			continue
		}
		for _, userID := range userIDs {
			p := &store.Progress{UserID: userID, HomeworkID: hw.ID, Status: d.status, UpdatedAt: now}
			if d.grade > 0 {
				p.Grade = &d.grade
			}
			if err := q.UpsertProgress(ctx, p); err != nil {
				return err
			}
		}
	}
	for _, d := range demoNotes {
		n := d
		n.GroupID, n.CreatedAt, n.UpdatedAt = groupID, now, now
		if err := q.CreateNote(ctx, &n); err != nil {
			return err
		}
	}
	for _, d := range demoClassLinks {
		l := &store.ClassLink{GroupID: groupID, SubjectID: subject(d.subject), LessonType: d.lesson, URL: d.url, Note: d.note}
		if err := q.CreateClassLink(ctx, l); err != nil {
			return err
		}
	}
	for _, d := range demoResources {
		l := &store.ResourceLink{GroupID: groupID, SubjectID: subject(d.subject), Kind: d.kind, Title: d.title, URL: d.url,
			Date: today.AddDate(0, 0, -d.daysAgo).Format(time.DateOnly), LessonType: d.lesson, CreatedAt: now}
		if err := q.CreateResourceLink(ctx, l); err != nil {
			return err
		}
	}
	return nil
}

// RunDemoReset resets the demo group now and then every night at
// demoResetHour, until ctx is done. Without a demo group it returns at once.
func (s *Service) RunDemoReset(ctx context.Context) {
	if s.cfg.DemoGroup == "" {
		return
	}
	for {
		s.resetDemoLogged(ctx)
		t := time.NewTimer(nextDemoReset(s.now(), s.cfg.Location).Sub(s.now()))
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
	}
}

func (s *Service) resetDemoLogged(ctx context.Context) {
	defer logPanic(ctx, "demo reset")
	// Demo accounts made here get passwords nobody knows; the superadmin
	// sets them with `extt admin reset-password`.
	if _, err := s.ResetDemo(ctx, s.cfg.DemoGroup); err != nil && ctx.Err() == nil {
		slog.ErrorContext(ctx, "demo reset", "group", s.cfg.DemoGroup, "err", err)
	}
}

// nextDemoReset is the first demoResetHour in loc after now.
func nextDemoReset(now time.Time, loc *time.Location) time.Time {
	day := StartOfDay(now, loc)
	next := time.Date(day.Year(), day.Month(), day.Day(), demoResetHour, 0, 0, 0, loc)
	if !next.After(now) {
		next = time.Date(day.Year(), day.Month(), day.Day()+1, demoResetHour, 0, 0, 0, loc)
	}
	return next
}
