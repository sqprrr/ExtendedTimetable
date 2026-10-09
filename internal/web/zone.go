package web

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/sqprrr/ExtendedTimetable/internal/i18n"
	"github.com/sqprrr/ExtendedTimetable/internal/service"
)

// zoneView is the time zone of a page, for the time zone picker, the note
// on the schedule pages and app.js, which reports the device's zone.
type zoneView struct {
	// Name is the zone the page's dates are in; Label is its city, "Warsaw",
	// and Offset its offset now, "UTC+2"; OffsetMinutes is that offset.
	Name, Label, Offset string
	OffsetMinutes       int
	// SiteLabel and SiteOffset are the site's zone, which classes run on,
	// when its clocks now show another time than Name's; "" otherwise.
	SiteLabel, SiteOffset string
	// Choice is the viewer's choice: service.TimeZoneAuto or a zone.
	Choice string
	// AutoLabel names the picker's automatic choice, with the device's zone
	// when it is known; Options are the zones it offers. Both are only
	// filled for signed-in users.
	AutoLabel string
	Options   []zoneOption
	// Cookie is the cookie in which app.js reports the device's zone, and
	// Device what it holds, as sent: app.js does not report it again.
	Cookie, Device string
	// Reload asks app.js to load the page again when the device's clock is
	// not Name's (OffsetMinutes): the page follows the device, is a GET and
	// is a signed-in user's (signed-out pages show no dates).
	Reload bool
}

// zoneOption is a time zone in the picker.
type zoneOption struct {
	Name, Label string
	Selected    bool
}

// pageLocation is the time zone a page is drawn in: the request's for a
// signed-in user, the site's for visitors. Signed-out pages show no dates,
// and another zone would cost a copy of the templates (Handler.templates),
// which anyone could otherwise make the server do with a device cookie.
func (h *Handler) pageLocation(r *http.Request) *time.Location {
	if service.ViewerFrom(r.Context()) == nil {
		return h.svc.SiteLocation()
	}
	return h.svc.Location(r.Context())
}

func (h *Handler) zoneView(r *http.Request, l *i18n.Localizer, loc *time.Location) zoneView {
	now := h.svc.Now()
	z := zoneView{
		Name: loc.String(), Label: zoneLabel(l, loc.String()), Offset: utcOffset(now.In(loc)),
		Choice: service.TimeZoneChoice(r.Context(), h.cookies.TimeZone(r)),
		Cookie: h.cookies.DeviceTimeZoneName(), Device: h.cookies.DeviceTimeZone(r),
	}
	site := h.svc.SiteLocation()
	_, off := now.In(loc).Zone()
	z.OffsetMinutes = off / 60
	if _, siteOff := now.In(site).Zone(); off != siteOff {
		z.SiteLabel, z.SiteOffset = zoneLabel(l, site.String()), utcOffset(now.In(site))
	}
	if service.ViewerFrom(r.Context()) == nil {
		return z
	}
	z.Reload = z.Choice == service.TimeZoneAuto && r.Method == http.MethodGet

	z.AutoLabel = l.T("tz.auto")
	if d := service.DeviceTimeZone(r.Context()); d != "" {
		z.AutoLabel = l.T("tz.auto_device", "Zone", zoneOptionLabel(l, d, now))
	}
	names := service.Zones
	if z.Choice != service.TimeZoneAuto && !slices.Contains(names, z.Choice) {
		names = append([]string{z.Choice}, names...)
	}
	z.Options = make([]zoneOption, 0, len(names))
	for _, name := range names {
		z.Options = append(z.Options, zoneOption{Name: name, Label: zoneOptionLabel(l, name, now), Selected: name == z.Choice})
	}
	return z
}

// zoneLabel names a time zone by its city: "Warsaw", translated for the
// zones of service.Zones; others by the last part of their name,
// "America/Argentina/Buenos_Aires" → "Buenos Aires".
func zoneLabel(l *i18n.Localizer, name string) string {
	if key, ok := service.ZoneKey(name); ok {
		return l.T("zone." + key)
	}
	return strings.ReplaceAll(name[strings.LastIndex(name, "/")+1:], "_", " ")
}

// zoneOptionLabel is a zone in the picker: "Warsaw (UTC+2)", with its
// offset at now.
func zoneOptionLabel(l *i18n.Localizer, name string, now time.Time) string {
	label, offset := zoneLabel(l, name), utcOffset(now.In(service.LoadTimeZone(name)))
	if label == offset {
		return label
	}
	return l.T("tz.option", "Zone", label, "Offset", offset)
}

// utcOffset is t's offset from UTC: "UTC+2", "UTC−4", "UTC+5:30", "UTC".
func utcOffset(t time.Time) string {
	_, off := t.Zone()
	if off == 0 {
		return "UTC"
	}
	sign := "+"
	if off < 0 {
		sign, off = "−", -off
	}
	if m := off % 3600 / 60; m != 0 {
		return fmt.Sprintf("UTC%s%d:%02d", sign, off/3600, m)
	}
	return fmt.Sprintf("UTC%s%d", sign, off/3600)
}

// setTimeZone changes the time zone, like setTheme: in a cookie for this
// browser and, for a signed-in user, in their account. An unknown zone
// falls back to the device's.
func (h *Handler) setTimeZone(w http.ResponseWriter, r *http.Request) {
	tz := r.PostFormValue("tz")
	if !service.IsTimeZoneChoice(tz) {
		tz = service.TimeZoneAuto
	}
	if service.ViewerFrom(r.Context()) != nil {
		if err := h.svc.SetTimeZone(r.Context(), tz); err != nil {
			h.renderError(w, r, err)
			return
		}
	}
	h.cookies.SetTimeZone(w, tz)
	back := r.PostFormValue("back")
	if !isLocalPath(back) {
		back = "/"
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}
