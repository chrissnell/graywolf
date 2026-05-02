package webapi

import (
	"errors"
	"net/http"
	"strconv"

	"gorm.io/gorm"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

// transcriptListLimit caps the size of GET /api/ax25/transcripts so a
// runaway recording history can't blow up the UI.
const transcriptListLimit = 500

func (s *Server) registerAX25Transcripts(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/ax25/transcripts", s.listAX25Transcripts)
	mux.HandleFunc("DELETE /api/ax25/transcripts", s.deleteAllAX25Transcripts)
	mux.HandleFunc("GET /api/ax25/transcripts/{id}", s.getAX25Transcript)
	mux.HandleFunc("DELETE /api/ax25/transcripts/{id}", s.deleteAX25Transcript)
}

// listAX25Transcripts returns recent transcript sessions (newest
// first), capped at transcriptListLimit.
//
// @Summary  List AX.25 transcript sessions
// @Tags     ax25
// @ID       listAX25Transcripts
// @Produce  json
// @Success  200 {array} dto.AX25TranscriptSession
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /ax25/transcripts [get]
func (s *Server) listAX25Transcripts(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.ListAX25TranscriptSessions(r.Context(), transcriptListLimit)
	if err != nil {
		s.internalError(w, r, "list ax25 transcripts", err)
		return
	}
	out := make([]dto.AX25TranscriptSession, 0, len(rows))
	for i := range rows {
		out = append(out, transcriptSessionToDTO(&rows[i]))
	}
	writeJSON(w, http.StatusOK, out)
}

// getAX25Transcript returns one transcript session plus its full entry
// list. Operators expand a row in the transcripts list to see this.
//
// @Summary  Get AX.25 transcript session detail
// @Tags     ax25
// @ID       getAX25Transcript
// @Produce  json
// @Param    id  path     uint32 true "Transcript session ID"
// @Success  200 {object} dto.AX25TranscriptDetail
// @Failure  404 {object} webtypes.ErrorResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /ax25/transcripts/{id} [get]
func (s *Server) getAX25Transcript(w http.ResponseWriter, r *http.Request) {
	id, ok := parseProfileID(w, r)
	if !ok {
		return
	}
	sess, err := s.store.GetAX25TranscriptSession(r.Context(), id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			notFound(w)
			return
		}
		s.internalError(w, r, "get ax25 transcript", err)
		return
	}
	rows, err := s.store.ListAX25TranscriptEntries(r.Context(), id)
	if err != nil {
		s.internalError(w, r, "list ax25 transcript entries", err)
		return
	}
	entries := make([]dto.AX25TranscriptEntry, 0, len(rows))
	for i := range rows {
		entries = append(entries, transcriptEntryToDTO(&rows[i]))
	}
	writeJSON(w, http.StatusOK, dto.AX25TranscriptDetail{
		Session: transcriptSessionToDTO(sess),
		Entries: entries,
	})
}

// deleteAX25Transcript removes a single session + its entries.
//
// @Summary  Delete AX.25 transcript session
// @Tags     ax25
// @ID       deleteAX25Transcript
// @Param    id path uint32 true "Transcript session ID"
// @Success  204
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /ax25/transcripts/{id} [delete]
func (s *Server) deleteAX25Transcript(w http.ResponseWriter, r *http.Request) {
	id, ok := parseProfileID(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteAX25TranscriptSession(r.Context(), id); err != nil {
		s.internalError(w, r, "delete ax25 transcript", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// deleteAllAX25Transcripts wipes every persisted transcript.
//
// @Summary  Delete every AX.25 transcript session
// @Tags     ax25
// @ID       deleteAllAX25Transcripts
// @Success  204
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /ax25/transcripts [delete]
func (s *Server) deleteAllAX25Transcripts(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteAllAX25Transcripts(r.Context()); err != nil {
		s.internalError(w, r, "delete all ax25 transcripts", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func transcriptSessionToDTO(s *configstore.AX25TranscriptSession) dto.AX25TranscriptSession {
	return dto.AX25TranscriptSession{
		ID:         s.ID,
		ChannelID:  s.ChannelID,
		PeerCall:   s.PeerCall,
		PeerSSID:   s.PeerSSID,
		ViaPath:    s.ViaPath,
		StartedAt:  s.StartedAt,
		EndedAt:    s.EndedAt,
		EndReason:  s.EndReason,
		ByteCount:  s.ByteCount,
		FrameCount: s.FrameCount,
	}
}

func transcriptEntryToDTO(e *configstore.AX25TranscriptEntry) dto.AX25TranscriptEntry {
	return dto.AX25TranscriptEntry{
		ID:        e.ID,
		TS:        e.TS,
		Direction: e.Direction,
		Kind:      e.Kind,
		Payload:   e.Payload,
	}
}

// parseProfileID is shared with ax25_profiles.go but since both files
// live in the same package the helper is reused as-is. The cast is
// safe because we only use it via the explicit caller.
var _ = strconv.ParseUint // keep strconv import balanced; helper file uses it
