package webapi

import (
	"net/http"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

func (s *Server) registerBulletinsConfig(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/bulletins/config", s.getBulletinsConfig)
	mux.HandleFunc("PUT /api/bulletins/config", s.putBulletinsConfig)
}

// getBulletinsConfig returns the singleton BulletinsConfig. Never 404s.
//
// @Summary  Get bulletins config
// @Tags     bulletins
// @ID       getBulletinsConfig
// @Produce  json
// @Success  200 {object} dto.BulletinsConfig
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /bulletins/config [get]
func (s *Server) getBulletinsConfig(w http.ResponseWriter, r *http.Request) {
	bc, err := s.store.GetBulletinsConfig(r.Context())
	if err != nil {
		s.internalError(w, r, "get bulletins config", err)
		return
	}
	writeJSON(w, http.StatusOK, dto.BulletinsConfig{
		TxChannel: bc.TxChannel,
		SendPath:  bc.SendPath,
	})
}

// putBulletinsConfig updates the singleton BulletinsConfig.
//
// @Summary  Update bulletins config
// @Tags     bulletins
// @ID       putBulletinsConfig
// @Accept   json
// @Produce  json
// @Param    body body     dto.BulletinsConfig true "Bulletins config"
// @Success  200  {object} dto.BulletinsConfig
// @Failure  400  {object} webtypes.ErrorResponse
// @Failure  500  {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /bulletins/config [put]
func (s *Server) putBulletinsConfig(w http.ResponseWriter, r *http.Request) {
	in, err := decodeJSON[dto.BulletinsConfig](r)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	if err := in.Validate(); err != nil {
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
	if err := s.store.UpsertBulletinsConfig(r.Context(), &configstore.BulletinsConfig{
		TxChannel: in.TxChannel,
		SendPath:  in.NormalizedSendPath(),
	}); err != nil {
		s.internalError(w, r, "upsert bulletins config", err)
		return
	}
	s.signalBulletinReload()
	persisted, err := s.store.GetBulletinsConfig(r.Context())
	if err != nil {
		s.internalError(w, r, "re-fetch bulletins config", err)
		return
	}
	writeJSON(w, http.StatusOK, dto.BulletinsConfig{
		TxChannel: persisted.TxChannel,
		SendPath:  persisted.SendPath,
	})
}
