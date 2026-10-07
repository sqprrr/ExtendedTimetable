package service

import "time"

// SetClock replaces the service's clock in tests.
func SetClock(s *Service, now func() time.Time) { s.now = now }
