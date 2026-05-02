package webapi

import (
	"errors"
	"net/http"
	"strconv"

	"gorm.io/gorm"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

func (s *Server) registerAX25Profiles(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/ax25/profiles", s.listAX25Profiles)
	mux.HandleFunc("POST /api/ax25/profiles", s.createAX25Profile)
	mux.HandleFunc("GET /api/ax25/profiles/{id}", s.getAX25Profile)
	mux.HandleFunc("PUT /api/ax25/profiles/{id}", s.updateAX25Profile)
	mux.HandleFunc("DELETE /api/ax25/profiles/{id}", s.deleteAX25Profile)
	mux.HandleFunc("POST /api/ax25/profiles/{id}/pin", s.pinAX25Profile)
}

// listAX25Profiles returns every saved profile, pinned first, then
// recents by last-used desc.
//
// @Summary  List AX.25 session profiles
// @Tags     ax25
// @ID       listAX25Profiles
// @Produce  json
// @Success  200 {array}  dto.AX25SessionProfile
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /ax25/profiles [get]
func (s *Server) listAX25Profiles(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.ListAX25SessionProfiles(r.Context())
	if err != nil {
		s.internalError(w, r, "list ax25 profiles", err)
		return
	}
	out := make([]dto.AX25SessionProfile, 0, len(rows))
	for i := range rows {
		out = append(out, profileToDTO(&rows[i]))
	}
	writeJSON(w, http.StatusOK, out)
}

// createAX25Profile inserts a new pinned profile.
//
// @Summary  Create AX.25 session profile
// @Tags     ax25
// @ID       createAX25Profile
// @Accept   json
// @Produce  json
// @Param    body body     dto.AX25SessionProfile true "Profile"
// @Success  201  {object} dto.AX25SessionProfile
// @Failure  400  {object} webtypes.ErrorResponse
// @Failure  500  {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /ax25/profiles [post]
func (s *Server) createAX25Profile(w http.ResponseWriter, r *http.Request) {
	in, err := decodeJSON[dto.AX25SessionProfile](r)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	if in.LocalCall == "" || in.DestCall == "" {
		badRequest(w, "local_call and dest_call are required")
		return
	}
	row := dtoToProfile(&in)
	row.ID = 0 // never let clients pick the id
	if err := s.store.CreateAX25SessionProfile(r.Context(), &row); err != nil {
		s.internalError(w, r, "create ax25 profile", err)
		return
	}
	writeJSON(w, http.StatusCreated, profileToDTO(&row))
}

// getAX25Profile returns a single profile by id.
//
// @Summary  Get AX.25 session profile
// @Tags     ax25
// @ID       getAX25Profile
// @Produce  json
// @Param    id   path     uint32 true "Profile ID"
// @Success  200 {object}  dto.AX25SessionProfile
// @Failure  404 {object}  webtypes.ErrorResponse
// @Failure  500 {object}  webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /ax25/profiles/{id} [get]
func (s *Server) getAX25Profile(w http.ResponseWriter, r *http.Request) {
	id, ok := parseProfileID(w, r)
	if !ok {
		return
	}
	row, err := s.store.GetAX25SessionProfile(r.Context(), id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			notFound(w)
			return
		}
		s.internalError(w, r, "get ax25 profile", err)
		return
	}
	writeJSON(w, http.StatusOK, profileToDTO(row))
}

// updateAX25Profile replaces the editable fields on an existing
// profile. Pinned + LastUsed are not touched here.
//
// @Summary  Update AX.25 session profile
// @Tags     ax25
// @ID       updateAX25Profile
// @Accept   json
// @Produce  json
// @Param    id   path     uint32                  true "Profile ID"
// @Param    body body     dto.AX25SessionProfile  true "Profile"
// @Success  200 {object}  dto.AX25SessionProfile
// @Failure  400 {object}  webtypes.ErrorResponse
// @Failure  404 {object}  webtypes.ErrorResponse
// @Failure  500 {object}  webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /ax25/profiles/{id} [put]
func (s *Server) updateAX25Profile(w http.ResponseWriter, r *http.Request) {
	id, ok := parseProfileID(w, r)
	if !ok {
		return
	}
	in, err := decodeJSON[dto.AX25SessionProfile](r)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	if in.LocalCall == "" || in.DestCall == "" {
		badRequest(w, "local_call and dest_call are required")
		return
	}
	row := dtoToProfile(&in)
	row.ID = id
	if err := s.store.UpdateAX25SessionProfile(r.Context(), &row); err != nil {
		s.internalError(w, r, "update ax25 profile", err)
		return
	}
	persisted, err := s.store.GetAX25SessionProfile(r.Context(), id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			notFound(w)
			return
		}
		s.internalError(w, r, "re-fetch ax25 profile", err)
		return
	}
	writeJSON(w, http.StatusOK, profileToDTO(persisted))
}

// deleteAX25Profile removes the row by id (idempotent).
//
// @Summary  Delete AX.25 session profile
// @Tags     ax25
// @ID       deleteAX25Profile
// @Param    id path uint32 true "Profile ID"
// @Success  204
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /ax25/profiles/{id} [delete]
func (s *Server) deleteAX25Profile(w http.ResponseWriter, r *http.Request) {
	id, ok := parseProfileID(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteAX25SessionProfile(r.Context(), id); err != nil {
		s.internalError(w, r, "delete ax25 profile", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// pinAX25Profile flips the Pinned flag on a profile, promoting it from
// recents into the permanent list.
//
// @Summary  Pin or unpin an AX.25 session profile
// @Tags     ax25
// @ID       pinAX25Profile
// @Accept   json
// @Produce  json
// @Param    id   path     uint32                       true "Profile ID"
// @Param    body body     dto.AX25SessionProfilePin    true "Pin payload"
// @Success  200  {object} dto.AX25SessionProfile
// @Failure  404  {object} webtypes.ErrorResponse
// @Failure  500  {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /ax25/profiles/{id}/pin [post]
func (s *Server) pinAX25Profile(w http.ResponseWriter, r *http.Request) {
	id, ok := parseProfileID(w, r)
	if !ok {
		return
	}
	in, err := decodeJSON[dto.AX25SessionProfilePin](r)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	if err := s.store.PinAX25SessionProfile(r.Context(), id, in.Pinned); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			notFound(w)
			return
		}
		s.internalError(w, r, "pin ax25 profile", err)
		return
	}
	persisted, err := s.store.GetAX25SessionProfile(r.Context(), id)
	if err != nil {
		s.internalError(w, r, "re-fetch ax25 profile", err)
		return
	}
	writeJSON(w, http.StatusOK, profileToDTO(persisted))
}

func parseProfileID(w http.ResponseWriter, r *http.Request) (uint32, bool) {
	idStr := r.PathValue("id")
	id64, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil || id64 == 0 {
		badRequest(w, "id must be a positive integer")
		return 0, false
	}
	return uint32(id64), true
}

func profileToDTO(p *configstore.AX25SessionProfile) dto.AX25SessionProfile {
	return dto.AX25SessionProfile{
		ID:        p.ID,
		Name:      p.Name,
		LocalCall: p.LocalCall,
		LocalSSID: p.LocalSSID,
		DestCall:  p.DestCall,
		DestSSID:  p.DestSSID,
		ViaPath:   p.ViaPath,
		Mod128:    p.Mod128,
		Paclen:    p.Paclen,
		Maxframe:  p.Maxframe,
		T1MS:      p.T1MS,
		T2MS:      p.T2MS,
		T3MS:      p.T3MS,
		N2:        p.N2,
		ChannelID: p.ChannelID,
		Pinned:    p.Pinned,
		LastUsed:  p.LastUsed,
	}
}

func dtoToProfile(in *dto.AX25SessionProfile) configstore.AX25SessionProfile {
	return configstore.AX25SessionProfile{
		Name:      in.Name,
		LocalCall: in.LocalCall,
		LocalSSID: in.LocalSSID,
		DestCall:  in.DestCall,
		DestSSID:  in.DestSSID,
		ViaPath:   in.ViaPath,
		Mod128:    in.Mod128,
		Paclen:    in.Paclen,
		Maxframe:  in.Maxframe,
		T1MS:      in.T1MS,
		T2MS:      in.T2MS,
		T3MS:      in.T3MS,
		N2:        in.N2,
		ChannelID: in.ChannelID,
	}
}
