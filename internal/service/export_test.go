package service

import "time"

// SetClock replaces the service's clock in tests.
func SetClock(s *Service, now func() time.Time) { s.now = now }

// LoginLimiterKeys counts the keys the login rate limiters keep by username.
func LoginLimiterKeys(s *Service) int { return s.loginByUsername.Len() }

// ParseRecordingTitle reads a video title as the playlist sync does.
var ParseRecordingTitle = parseRecordingTitle
