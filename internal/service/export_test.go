package service

import "time"

// SetClock replaces the service's clock in tests.
func SetClock(s *Service, now func() time.Time) { s.now = now }

// LoginLimiterKeys counts the keys the login rate limiters keep by username.
func LoginLimiterKeys(s *Service) int { return s.loginByUserAndIP.Len() + s.loginByUsername.Len() }

// ParseRecordingTitle reads a video title as the playlist sync does.
var ParseRecordingTitle = parseRecordingTitle

// ParseRecording reads a video's title and description as the playlist
// sync does.
var ParseRecording = parseRecording

// PickNowClass and LaterToday are nowClass and laterToday.
var (
	PickNowClass = nowClass
	LaterToday   = laterToday
)

// TelegramUsername reads the Telegram field of the group request form.
var TelegramUsername = telegramUsername

// GroupCode spells a group's name as a group code.
var GroupCode = groupCode

// NextDemoReset is when the demo group is next reset.
var NextDemoReset = nextDemoReset
