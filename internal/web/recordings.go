package web

import (
	"errors"
	"net/http"

	"github.com/sqprrr/ExtendedTimetable/internal/service"
)

// The recordings page holds, for the leader and editors, the YouTube
// playlists the group's recordings are synced from.

// addPlaylist adds a playlist and syncs the group's playlists.
func (h *Handler) addPlaylist(w http.ResponseWriter, r *http.Request) {
	if g := h.groupPage(w, r); g != nil {
		input := r.PostFormValue("playlist")
		h.afterRecordingSync(w, r, g, h.svc.AddPlaylist(r.Context(), g.ID, input), input)
	}
}

// removePlaylist stops syncing a playlist; its recordings stay.
func (h *Handler) removePlaylist(w http.ResponseWriter, r *http.Request) {
	if g := h.groupPage(w, r); g != nil {
		if err := h.svc.RemovePlaylist(r.Context(), g.ID, r.PathValue("playlist")); err != nil {
			h.renderError(w, r, err)
			return
		}
		http.Redirect(w, r, sectionURL(g, "resources"), http.StatusSeeOther)
	}
}

// syncRecordings syncs the group's playlist now.
func (h *Handler) syncRecordings(w http.ResponseWriter, r *http.Request) {
	if g := h.groupPage(w, r); g != nil {
		_, err := h.svc.SyncRecordingsNow(r.Context(), g.ID)
		h.afterRecordingSync(w, r, g, err, "")
	}
}

// afterRecordingSync goes back to the recordings, which show how the sync
// went; a failed fetch is shown there too. Other errors the user can act on
// are shown on the page with what they typed.
func (h *Handler) afterRecordingSync(w http.ResponseWriter, r *http.Request, g *service.GroupView, err error, input string) {
	if serr := new(service.SyncError); err == nil || errors.As(err, &serr) {
		http.Redirect(w, r, sectionURL(g, "resources"), http.StatusSeeOther)
		return
	}
	msg, status, ok := userMessage(r, err)
	if !ok {
		h.renderError(w, r, err)
		return
	}
	for _, s := range h.sections() {
		if s.name == "resources" {
			h.showSection(w, r, s, g, status, pageData{Error: msg, PlaylistInput: input})
			return
		}
	}
}
