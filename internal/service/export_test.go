package service

import "time"

// SetClock replaces the service's clock in tests.
func SetClock(s *Service, now func() time.Time) { s.now = now }

// ParseRecordingTitle reads a video title as the playlist sync does.
var ParseRecordingTitle = parseRecordingTitle
