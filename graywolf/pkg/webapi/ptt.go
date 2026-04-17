package webapi

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"runtime"
	"strings"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/pttdevice"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

func (s *Server) registerPtt(mux *http.ServeMux) {
	mux.HandleFunc("/api/ptt", s.handlePttCollection)
	mux.HandleFunc("/api/ptt/", s.handlePttByChannel)
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

// GET/POST /api/ptt
func (s *Server) handlePttCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		handleList[configstore.PttConfig](s, w, r, "list ptt configs",
			s.store.ListPttConfigs, dto.PttFromModel)
	case http.MethodPost:
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
	default:
		methodNotAllowed(w)
	}
}

// /api/ptt/{channel}      — GET, PUT, DELETE
// /api/ptt/available      — GET device enumeration (flat array of devices)
// /api/ptt/capabilities   — GET platform PTT capability flags
// /api/ptt/test-rigctld   — POST probe a rigctld endpoint (see ptt_test_rigctld.go)
// /api/ptt/gpio-lines?chip=/dev/gpiochipN — GET enumerate GPIO lines (Linux only)
//
// `capabilities` and `gpio-lines` are peer endpoints rather than nested
// under `available` so /api/ptt/available stays a backwards-compatible
// flat device list for clients that don't need the new data yet.
func (s *Server) handlePttByChannel(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/ptt/")
	if rest == "available" {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		writeJSON(w, http.StatusOK, pttdevice.Enumerate())
		return
	}
	if rest == "capabilities" {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		writeJSON(w, http.StatusOK, pttCapabilities{
			PlatformSupportsGpio: runtime.GOOS == "linux",
		})
		return
	}
	if rest == "test-rigctld" {
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		s.handleTestRigctld(w, r)
		return
	}
	if rest == "gpio-lines" {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		chip := r.URL.Query().Get("chip")
		if chip == "" {
			badRequest(w, "chip query parameter required")
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
					map[string]string{"error": "gpio line enumeration requires linux"})
				return
			}
			// Surface permission failures with an actionable hint —
			// the most common deployment mistake is the service user
			// missing the gpio group. The chip path is user-supplied
			// but limited to a /dev/gpiochipN pattern in practice;
			// echoing it back helps the admin know which device is
			// inaccessible.
			if errors.Is(err, fs.ErrPermission) {
				s.logger.WarnContext(r.Context(), "gpio chip access denied",
					"chip", chip, "err", err)
				writeJSON(w, http.StatusForbidden, map[string]string{
					"error": "permission denied on " + chip + " — the graywolf service needs membership in the 'gpio' group (Raspberry Pi OS/Debian) or equivalent on your distro",
				})
				return
			}
			s.internalError(w, r, "enumerate gpio lines", err)
			return
		}
		writeJSON(w, http.StatusOK, lines)
		return
	}
	id, err := parseID(rest)
	if err != nil {
		badRequest(w, "invalid channel id")
		return
	}
	switch r.Method {
	case http.MethodGet:
		handleGet[*configstore.PttConfig](s, w, r, id,
			s.store.GetPttConfigForChannel,
			func(p *configstore.PttConfig) dto.PttResponse {
				return dto.PttFromModel(*p)
			})
	case http.MethodPut:
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
	case http.MethodDelete:
		handleDelete(s, w, r, "delete ptt config", id, func(ctx context.Context, channelID uint32) error {
			if err := s.store.DeletePttConfig(ctx, channelID); err != nil {
				return err
			}
			s.notifyBridgeForChannel(ctx, channelID)
			return nil
		})
	default:
		methodNotAllowed(w)
	}
}
