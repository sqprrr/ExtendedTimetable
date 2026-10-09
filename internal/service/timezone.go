package service

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	_ "time/tzdata" // users' zones must not depend on the host's zoneinfo
)

// Classes run on the site's time (Config.Location, Kyiv), but students are
// not all there: each user sees and enters dates in a time zone of their
// own. It is the zone they chose, or TimeZoneAuto: their device's, which
// the browser reports, else the site's.

// TimeZoneAuto is the time zone choice that follows the device; it is also
// what a user who never chose gets.
const TimeZoneAuto = "auto"

// Zones are the time zones the picker offers, roughly west to east: the
// site's and those students abroad are most likely to be in. Any other
// IANA zone still works when a device reports it or the API sets it.
var Zones = []string{
	"America/Los_Angeles", "America/Chicago", "America/New_York", "America/Toronto",
	"UTC", "Europe/Lisbon", "Europe/London", "Europe/Dublin",
	"Europe/Madrid", "Europe/Paris", "Europe/Brussels", "Europe/Amsterdam", "Europe/Berlin",
	"Europe/Copenhagen", "Europe/Oslo", "Europe/Stockholm", "Europe/Zurich", "Europe/Rome",
	"Europe/Vienna", "Europe/Prague", "Europe/Bratislava", "Europe/Warsaw", "Europe/Budapest",
	"Europe/Vilnius", "Europe/Riga", "Europe/Tallinn", "Europe/Helsinki", "Europe/Bucharest",
	"Europe/Chisinau", "Europe/Sofia", "Europe/Athens", "Europe/Kyiv", "Europe/Istanbul",
	"Asia/Jerusalem", "Asia/Tbilisi", "Asia/Dubai", "Asia/Tokyo", "Australia/Sydney",
}

// ZoneKey names a zone of Zones in the locale files (tz.<key>): its city,
// "Europe/New_York" → "new_york". ok is false for zones not in Zones.
func ZoneKey(name string) (key string, ok bool) {
	if name == "Europe/Kiev" { // the old name, which some browsers still report
		return "kyiv", true
	}
	for _, z := range Zones {
		if z == name {
			return strings.ToLower(name[strings.LastIndex(name, "/")+1:]), true
		}
	}
	return "", false
}

// maxZones bounds the cache of loaded zones; there are about 600 names,
// but a case-insensitive file system would accept many spellings of each.
const maxZones = 1000

var (
	zoneCache sync.Map // name → *time.Location
	zoneCount atomic.Int64
)

// LoadTimeZone returns the IANA time zone name, or nil when it is not one.
// "" and "Local" (UTC and the server's zone to time.LoadLocation) are not.
// Europe/Kiev loads as Europe/Kyiv, so it matches the site's zone.
func LoadTimeZone(name string) *time.Location {
	if name == "" || name == "Local" || len(name) > 64 {
		return nil
	}
	if loc, ok := zoneCache.Load(name); ok {
		return loc.(*time.Location)
	}
	load := name
	if load == "Europe/Kiev" { // the old name, which some browsers still report
		load = "Europe/Kyiv"
	}
	loc, err := time.LoadLocation(load)
	if err != nil {
		return nil
	}
	if zoneCount.Load() < maxZones {
		if _, loaded := zoneCache.LoadOrStore(name, loc); !loaded {
			zoneCount.Add(1)
		}
	}
	return loc
}

// IsTimeZoneChoice reports whether tz is a time zone a user can choose:
// TimeZoneAuto or an IANA zone.
func IsTimeZoneChoice(tz string) bool { return tz == TimeZoneAuto || LoadTimeZone(tz) != nil }

// chosenTimeZone returns tz if it is a time zone choice, or "" (not chosen).
func chosenTimeZone(tz string) string {
	if IsTimeZoneChoice(tz) {
		return tz
	}
	return ""
}

// TimeZoneChoice is the request's time zone choice: the signed-in user's,
// else cookie (this browser's), else TimeZoneAuto.
func TimeZoneChoice(ctx context.Context, cookie string) string {
	if v := ViewerFrom(ctx); v != nil && IsTimeZoneChoice(v.TimeZone) {
		return v.TimeZone
	}
	if IsTimeZoneChoice(cookie) {
		return cookie
	}
	return TimeZoneAuto
}

// resolveLocation returns the time zone of a choice: the chosen zone or, for
// TimeZoneAuto, the device's, else the site's.
func (s *Service) resolveLocation(choice, device string) *time.Location {
	if choice != TimeZoneAuto {
		if loc := LoadTimeZone(choice); loc != nil {
			return loc
		}
	}
	if loc := LoadTimeZone(device); loc != nil {
		return loc
	}
	return s.cfg.Location
}

// SetTimeZone stores the signed-in viewer's time zone choice.
func (s *Service) SetTimeZone(ctx context.Context, tz string) error {
	v, err := requireViewer(ctx)
	if err != nil {
		return err
	}
	if !IsTimeZoneChoice(tz) {
		return inputError("time_zone", "err.time_zone")
	}
	if err := s.store.SetTimeZone(ctx, v.UserID, tz); err != nil {
		return err
	}
	v.TimeZone = tz
	return nil
}

type zoneKey struct{}

// requestZone is the time zone of a request and the device's, if known.
type requestZone struct {
	loc    *time.Location
	device string
}

// WithTimeZone returns a context whose dates are shown, entered and cut
// into days in the time zone of choice (see TimeZoneChoice). device is the
// zone the device reported, "" if it did not.
func (s *Service) WithTimeZone(ctx context.Context, choice, device string) context.Context {
	if LoadTimeZone(device) == nil {
		device = ""
	}
	return context.WithValue(ctx, zoneKey{}, requestZone{loc: s.resolveLocation(choice, device), device: device})
}

// Location is the request's time zone (WithTimeZone), else the site's.
func (s *Service) Location(ctx context.Context) *time.Location {
	if z, ok := ctx.Value(zoneKey{}).(requestZone); ok {
		return z.loc
	}
	return s.cfg.Location
}

// DeviceTimeZone is the time zone the request's device reported, or "".
func DeviceTimeZone(ctx context.Context) string {
	z, _ := ctx.Value(zoneKey{}).(requestZone)
	return z.device
}

// SiteLocation is the site's time zone: the one classes run on, and the
// default for users.
func (s *Service) SiteLocation() *time.Location { return s.cfg.Location }
