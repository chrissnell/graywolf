package webapi

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"net/url"
	"runtime"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/pttdevice"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
	"github.com/chrissnell/graywolf/pkg/webtypes"
)

// registerPtt installs the /api/ptt route tree on mux using Go 1.22+
// method-scoped patterns. Literal subpaths (available, capabilities,
// test-rigctld, gpio-chips/{chip}/lines) are registered alongside the
// {channel} catch-all; Go's mux prefers literal segments over
// wildcards, so requests like GET /api/ptt/available reach
// listPttDevices rather than being parsed as channel id "available".
//
// Breaking change from the previous hand-rolled dispatcher: GPIO line
// enumeration moved from
//
//	GET /api/ptt/gpio-lines?chip=/dev/gpiochipN
//
// to
//
//	GET /api/ptt/gpio-chips/{chip}/lines
//
// where {chip} is the URL-encoded device path (e.g. %2Fdev%2Fgpiochip0).
// This keeps the OpenAPI spec clean — no query-string-smuggled device
// paths — at the cost of a one-time UI call-site update.
//
// Operation IDs used in the swag annotation blocks below are frozen
// against the constants in pkg/webapi/docs/op_ids.go.
func (s *Server) registerPtt(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/ptt", s.listPttConfigs)
	mux.HandleFunc("POST /api/ptt", s.upsertPttConfig)
	mux.HandleFunc("GET /api/ptt/available", s.listPttDevices)
	mux.HandleFunc("GET /api/ptt/capabilities", s.getPttCapabilities)
	mux.HandleFunc("POST /api/ptt/test-rigctld", s.testRigctld)
	mux.HandleFunc("GET /api/ptt/gpio-chips/{chip}/lines", s.listGpioLines)
	mux.HandleFunc("GET /api/ptt/{channel}", s.getPttConfig)
	mux.HandleFunc("PUT /api/ptt/{channel}", s.updatePttConfig)
	mux.HandleFunc("DELETE /api/ptt/{channel}", s.deletePttConfig)
}

// pttCapabilities carries platform-level PTT feature flags. Values are
// runtime-derived and stable for the lifetime of the process, so the
// UI can fetch this once at startup.
type pttCapabilities struct {
	// PlatformSupportsGpio is true on Linux, where the gpiochip v2
	// character-device API is available. The UI consults this flag —
	// not the presence of enumerated chips — to decide whether the
	// GPIO method appears in its dropdown, so a Linux host without
	// any detected chips still shows GPIO with an explained empty
	// state rather than silently omitting the option.
	PlatformSupportsGpio bool `json:"platform_supports_gpio"`
}

// listPttConfigs returns every configured PTT entry.
//
// @Summary  List PTT configs
// @Tags     ptt
// @ID       listPttConfigs
// @Produce  json
// @Success  200 {array}  dto.PttResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /ptt [get]
func (s *Server) listPttConfigs(w http.ResponseWriter, r *http.Request) {
	handleList[configstore.PttConfig](s, w, r, "list ptt configs",
		s.store.ListPttConfigs, dto.PttFromModel)
}

// upsertPttConfig creates or replaces a PTT config by channel_id.
// The underlying store treats the channel_id in the body as the upsert
// key, so this endpoint stays at /api/ptt (no {channel} in the path)
// and returns 201 with the persisted record.
//
// @Summary  Upsert PTT config
// @Tags     ptt
// @ID       upsertPttConfig
// @Accept   json
// @Produce  json
// @Param    body body     dto.PttRequest true "PTT config"
// @Success  201  {object} dto.PttResponse
// @Failure  400  {object} webtypes.ErrorResponse
// @Failure  500  {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /ptt [post]
func (s *Server) upsertPttConfig(w http.ResponseWriter, r *http.Request) {
	handleCreate[dto.PttRequest](s, w, r, "upsert ptt config",
		func(ctx context.Context, req dto.PttRequest) (configstore.PttConfig, error) {
			m := req.ToModel()
			if err := s.store.UpsertPttConfig(ctx, &m); err != nil {
				return configstore.PttConfig{}, err
			}
			s.notifyBridgeForChannel(ctx, m.ChannelID)
			return m, nil
		},
		dto.PttFromModel)
}

// listPttDevices enumerates PTT-capable devices detected on the host —
// serial ports, gpiochips, and CM108 HID devices. The payload is a flat
// array of devices regardless of platform; platform-dependent fields
// like `warning` and `recommended` are populated by the enumerator.
//
// @Summary  List PTT devices
// @Tags     ptt
// @ID       listPttDevices
// @Produce  json
// @Success  200 {array}  pttdevice.AvailableDevice
// @Security CookieAuth
// @Router   /ptt/available [get]
func (s *Server) listPttDevices(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, pttdevice.Enumerate())
}

// getPttCapabilities reports platform-level PTT capability flags. The
// UI fetches this at startup to gate method-specific dropdowns (e.g.
// GPIO is only offered on Linux).
//
// @Summary  Get PTT capabilities
// @Tags     ptt
// @ID       getPttCapabilities
// @Produce  json
// @Success  200 {object} webapi.pttCapabilities
// @Security CookieAuth
// @Router   /ptt/capabilities [get]
func (s *Server) getPttCapabilities(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, pttCapabilities{
		PlatformSupportsGpio: runtime.GOOS == "linux",
	})
}

// testRigctld probes a rigctld endpoint on behalf of the UI's "Test
// Connection" button. The bulk of the handler body (TCP dial + probe)
// lives in ptt_test_rigctld.go. See that file for the full protocol
// and error-mapping contract.
//
// @Summary  Test rigctld connection
// @Tags     ptt
// @ID       testRigctld
// @Accept   json
// @Produce  json
// @Param    body body     dto.TestRigctldRequest true "rigctld endpoint"
// @Success  200  {object} dto.TestRigctldResponse
// @Failure  400  {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /ptt/test-rigctld [post]
func (s *Server) testRigctld(w http.ResponseWriter, r *http.Request) {
	s.handleTestRigctld(w, r)
}

// listGpioLines enumerates the lines of a gpiochip character device.
// The {chip} path parameter is the URL-encoded absolute device path
// (e.g. %2Fdev%2Fgpiochip0 for /dev/gpiochip0). Go's ServeMux already
// unescapes path values, but we run the result through url.PathUnescape
// anyway so clients that double-encode the path (a reasonable defensive
// practice given the embedded slashes) still produce the correct
// device path on the server side.
//
// Error mapping:
//   - missing/empty {chip}         → 400 (defense in depth; the mux
//     requires a non-empty segment)
//   - non-Linux host               → 501 "gpio line enumeration requires linux"
//   - permission denied on the chip → 403 with a hint about gpio group
//     membership
//   - any other failure            → 500 via internalError
//
// @Summary  List GPIO lines
// @Tags     ptt
// @ID       listGpioLines
// @Produce  json
// @Param    chip path     string true "URL-encoded gpiochip device path (e.g. %2Fdev%2Fgpiochip0)"
// @Success  200  {array}  pttdevice.GpioLineInfo
// @Failure  400  {object} webtypes.ErrorResponse
// @Failure  403  {object} webtypes.ErrorResponse
// @Failure  500  {object} webtypes.ErrorResponse
// @Failure  501  {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /ptt/gpio-chips/{chip}/lines [get]
func (s *Server) listGpioLines(w http.ResponseWriter, r *http.Request) {
	chip, err := url.PathUnescape(r.PathValue("chip"))
	if err != nil || chip == "" {
		badRequest(w, "chip is required")
		return
	}
	lines, err := pttdevice.EnumerateGpioLines(chip)
	if err != nil {
		// On non-Linux hosts EnumerateGpioLines always fails with a
		// fixed "only supported on Linux" message; map that to 501
		// so clients can distinguish a platform limitation from a
		// genuine server fault.
		if runtime.GOOS != "linux" {
			s.logger.InfoContext(r.Context(), "gpio lines requested on non-linux",
				"chip", chip, "err", err)
			writeJSON(w, http.StatusNotImplemented,
				webtypes.ErrorResponse{Error: "gpio line enumeration requires linux"})
			return
		}
		// Surface permission failures with an actionable hint — the
		// most common deployment mistake is the service user missing
		// the gpio group. The chip path is user-supplied but limited
		// to a /dev/gpiochipN pattern in practice; echoing it back
		// helps the admin know which device is inaccessible.
		if errors.Is(err, fs.ErrPermission) {
			s.logger.WarnContext(r.Context(), "gpio chip access denied",
				"chip", chip, "err", err)
			writeJSON(w, http.StatusForbidden, webtypes.ErrorResponse{
				Error: "permission denied on " + chip + " — the graywolf service needs membership in the 'gpio' group (Raspberry Pi OS/Debian) or equivalent on your distro",
			})
			return
		}
		s.internalError(w, r, "enumerate gpio lines", err)
		return
	}
	writeJSON(w, http.StatusOK, lines)
}

// getPttConfig returns the PTT config for a channel.
//
// @Summary  Get PTT config
// @Tags     ptt
// @ID       getPttConfig
// @Produce  json
// @Param    channel path     int true "Channel id"
// @Success  200     {object} dto.PttResponse
// @Failure  400     {object} webtypes.ErrorResponse
// @Failure  404     {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /ptt/{channel} [get]
func (s *Server) getPttConfig(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("channel"))
	if err != nil {
		badRequest(w, "invalid channel id")
		return
	}
	handleGet[*configstore.PttConfig](s, w, r, "get ptt config", id,
		s.store.GetPttConfigForChannel,
		func(p *configstore.PttConfig) dto.PttResponse {
			return dto.PttFromModel(*p)
		})
}

// updatePttConfig upserts the PTT config for a channel, pinning the
// channel id from the URL over anything the body carries.
//
// @Summary  Update PTT config
// @Tags     ptt
// @ID       updatePttConfig
// @Accept   json
// @Produce  json
// @Param    channel path     int            true "Channel id"
// @Param    body    body     dto.PttRequest true "PTT config"
// @Success  200     {object} dto.PttResponse
// @Failure  400     {object} webtypes.ErrorResponse
// @Failure  500     {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /ptt/{channel} [put]
func (s *Server) updatePttConfig(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("channel"))
	if err != nil {
		badRequest(w, "invalid channel id")
		return
	}
	handleUpdate[dto.PttRequest](s, w, r, "upsert ptt config", id,
		func(ctx context.Context, channelID uint32, req dto.PttRequest) (configstore.PttConfig, error) {
			m := req.ToUpdate(channelID)
			if err := s.store.UpsertPttConfig(ctx, &m); err != nil {
				return configstore.PttConfig{}, err
			}
			s.notifyBridgeForChannel(ctx, channelID)
			return m, nil
		},
		dto.PttFromModel)
}

// deletePttConfig removes the PTT config for a channel.
//
// @Summary  Delete PTT config
// @Tags     ptt
// @ID       deletePttConfig
// @Param    channel path int true "Channel id"
// @Success  204     "No Content"
// @Failure  400     {object} webtypes.ErrorResponse
// @Failure  500     {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /ptt/{channel} [delete]
func (s *Server) deletePttConfig(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("channel"))
	if err != nil {
		badRequest(w, "invalid channel id")
		return
	}
	handleDelete(s, w, r, "delete ptt config", id, func(ctx context.Context, channelID uint32) error {
		if err := s.store.DeletePttConfig(ctx, channelID); err != nil {
			return err
		}
		s.notifyBridgeForChannel(ctx, channelID)
		return nil
	})
}
