package webapi

import (
	"errors"
	"net/http"

	"gorm.io/gorm"

	"github.com/chrissnell/graywolf/pkg/actions"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

func (s *Server) registerActionTestFire(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/actions/{id}/test-fire", s.testFireAction)
}

// testFireAction runs the given Action through its executor inline,
// bypassing the OTP requirement and sender allowlist (operator
// authority is the cookie session). Sanitization still runs; an
// audit row is written with sender_call="(test-web)".
//
// @Summary  Test-fire an action
// @Tags     actions
// @ID       testFireAction
// @Accept   json
// @Produce  json
// @Param    id   path     int                  true "Action id"
// @Param    body body     dto.TestFireRequest  true "Args"
// @Success  200  {object} dto.TestFireResponse
// @Failure  400  {object} webtypes.ErrorResponse
// @Failure  404  {object} webtypes.ErrorResponse
// @Failure  500  {object} webtypes.ErrorResponse
// @Failure  503  {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /actions/{id}/test-fire [post]
func (s *Server) testFireAction(w http.ResponseWriter, r *http.Request) {
	if s.actions == nil {
		serviceUnavailable(w, "actions service not available")
		return
	}
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	a, err := s.store.GetAction(r.Context(), uint(id))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			notFound(w)
			return
		}
		s.internalError(w, r, "get action", err)
		return
	}
	in, err := decodeJSON[dto.TestFireRequest](r)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	schema, err := actions.DecodeArgSchemaJSON(a.ArgSchema)
	if err != nil {
		s.internalError(w, r, "decode arg schema", err)
		return
	}
	clean, sErr := actions.SanitizeFromMap(schema, in.Args)
	if sErr != nil {
		// Match the on-air reply wording so the UI presents the same
		// failure shape.
		key := actions.BadArgKey(sErr)
		msg := "bad arg"
		if key != "" {
			msg = "bad arg: " + key
		}
		badRequest(w, msg)
		return
	}
	res, invID := s.actions.TestFire(r.Context(), a, clean)
	reply, _ := actions.FormatReply(res)
	writeJSON(w, http.StatusOK, dto.TestFireResponse{
		Status:        string(res.Status),
		StatusDetail:  res.StatusDetail,
		OutputCapture: res.OutputCapture,
		ReplyText:     reply,
		ExitCode:      res.ExitCode,
		HTTPStatus:    res.HTTPStatus,
		InvocationID:  invID,
	})
}
