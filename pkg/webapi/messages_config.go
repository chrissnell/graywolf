package webapi

import (
	"net/http"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

func (s *Server) registerMessagesConfig(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/messages/config", s.getMessagesConfig)
	mux.HandleFunc("PUT /api/messages/config", s.putMessagesConfig)
}

// getMessagesConfig returns the singleton MessagesConfig. The store
// auto-creates an empty row on first read, so this never 404s.
//
// @Summary  Get messages config
// @Tags     messages
// @ID       getMessagesConfig
// @Produce  json
// @Success  200 {object} dto.MessagesConfig
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /messages/config [get]
func (s *Server) getMessagesConfig(w http.ResponseWriter, r *http.Request) {
	mc, err := s.store.GetMessagesConfig(r.Context())
	if err != nil {
		s.internalError(w, r, "get messages config", err)
		return
	}
	writeJSON(w, http.StatusOK, dto.MessagesConfig{TxChannel: mc.TxChannel})
}

// putMessagesConfig updates the singleton MessagesConfig. Rejects a
// tx_channel whose Channel.Mode is "packet".
//
// @Summary  Update messages config
// @Tags     messages
// @ID       putMessagesConfig
// @Accept   json
// @Produce  json
// @Param    body body     dto.MessagesConfig true "Messages config"
// @Success  200  {object} dto.MessagesConfig
// @Failure  400  {object} webtypes.ErrorResponse
// @Failure  500  {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /messages/config [put]
func (s *Server) putMessagesConfig(w http.ResponseWriter, r *http.Request) {
	in, err := decodeJSON[dto.MessagesConfig](r)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	if in.TxChannel != 0 {
		mode, err := s.store.ModeForChannel(r.Context(), in.TxChannel)
		if err != nil {
			s.internalError(w, r, "mode-for-channel lookup", err)
			return
		}
		if mode == configstore.ChannelModePacket {
			badRequest(w, "tx_channel is packet-mode; choose aprs or aprs+packet")
			return
		}
	}
	if err := s.store.UpsertMessagesConfig(r.Context(), &configstore.MessagesConfig{
		TxChannel: in.TxChannel,
	}); err != nil {
		s.internalError(w, r, "upsert messages config", err)
		return
	}
	s.signalMessagesReload()
	// Re-fetch the persisted row rather than echoing the request body,
	// so the response always reflects what the store stored (timestamps,
	// any future server-side normalization).
	persisted, err := s.store.GetMessagesConfig(r.Context())
	if err != nil {
		s.internalError(w, r, "re-fetch messages config", err)
		return
	}
	writeJSON(w, http.StatusOK, dto.MessagesConfig{TxChannel: persisted.TxChannel})
}
