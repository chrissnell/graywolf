package diagcollect

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/flareschema"
)

// CollectConfig dumps every typed object in the configstore into a
// flat key/value list, applying key-based scrub rules at source.
//
// A nil store returns an empty section + a config_db_unavailable issue.
func CollectConfig(ctx context.Context, store *configstore.Store) flareschema.ConfigSection {
	if store == nil {
		return flareschema.ConfigSection{
			Issues: []flareschema.CollectorIssue{{
				Kind:    "config_db_unavailable",
				Message: "configstore not opened (graywolf.db missing or unreadable)",
			}},
		}
	}

	var section flareschema.ConfigSection
	emit := func(key, value string) {
		if shouldDropKey(key) {
			return
		}
		section.Items = append(section.Items, flareschema.ConfigItem{
			Key:   key,
			Value: scrubKeyValue(key, value),
		})
	}

	type loader struct {
		prefix string
		fn     func(context.Context, *configstore.Store) (any, error)
	}

	// Singleton loaders. GetStationConfig/GetUpdatesConfig/GetThemeConfig/
	// GetUnitsConfig/GetMapsConfig return value types; wrapping in any is fine.
	// Pointer-returning singletons return nil on no-row, handled below.
	loaders := []loader{
		{"station", func(c context.Context, s *configstore.Store) (any, error) {
			v, err := s.GetStationConfig(c)
			return v, err
		}},
		{"updates", func(c context.Context, s *configstore.Store) (any, error) {
			v, err := s.GetUpdatesConfig(c)
			return v, err
		}},
		{"theme", func(c context.Context, s *configstore.Store) (any, error) {
			v, err := s.GetThemeConfig(c)
			return v, err
		}},
		{"units", func(c context.Context, s *configstore.Store) (any, error) {
			v, err := s.GetUnitsConfig(c)
			return v, err
		}},
		{"maps", func(c context.Context, s *configstore.Store) (any, error) {
			v, err := s.GetMapsConfig(c)
			return v, err
		}},
		// GetLogBufferConfig returns (LogBufferConfig, bool, error); discard the bool.
		{"logbuffer", func(c context.Context, s *configstore.Store) (any, error) {
			v, _, err := s.GetLogBufferConfig(c)
			return v, err
		}},
		{"agw", func(c context.Context, s *configstore.Store) (any, error) {
			return s.GetAgwConfig(c)
		}},
		{"digipeater", func(c context.Context, s *configstore.Store) (any, error) {
			return s.GetDigipeaterConfig(c)
		}},
		{"igate", func(c context.Context, s *configstore.Store) (any, error) {
			return s.GetIGateConfig(c)
		}},
		{"gps", func(c context.Context, s *configstore.Store) (any, error) {
			return s.GetGPSConfig(c)
		}},
		{"smart_beacon", func(c context.Context, s *configstore.Store) (any, error) {
			return s.GetSmartBeaconConfig(c)
		}},
		{"position_log", func(c context.Context, s *configstore.Store) (any, error) {
			return s.GetPositionLogConfig(c)
		}},
		{"messages_prefs", func(c context.Context, s *configstore.Store) (any, error) {
			return s.GetMessagePreferences(c)
		}},
		// List loaders.
		{"channels", func(c context.Context, s *configstore.Store) (any, error) {
			return s.ListChannels(c)
		}},
		{"audio_devices", func(c context.Context, s *configstore.Store) (any, error) {
			return s.ListAudioDevices(c)
		}},
		{"kiss_interfaces", func(c context.Context, s *configstore.Store) (any, error) {
			return s.ListKissInterfaces(c)
		}},
		{"ptt_configs", func(c context.Context, s *configstore.Store) (any, error) {
			return s.ListPttConfigs(c)
		}},
		{"tx_timings", func(c context.Context, s *configstore.Store) (any, error) {
			return s.ListTxTimings(c)
		}},
		{"digipeater_rules", func(c context.Context, s *configstore.Store) (any, error) {
			return s.ListDigipeaterRules(c)
		}},
		{"igate_rf_filters", func(c context.Context, s *configstore.Store) (any, error) {
			return s.ListIGateRfFilters(c)
		}},
		{"beacons", func(c context.Context, s *configstore.Store) (any, error) {
			return s.ListBeacons(c)
		}},
		{"packet_filters", func(c context.Context, s *configstore.Store) (any, error) {
			return s.ListPacketFilters(c)
		}},
		{"tactical_callsigns", func(c context.Context, s *configstore.Store) (any, error) {
			return s.ListTacticalCallsigns(c)
		}},
	}

	for _, ld := range loaders {
		val, err := ld.fn(ctx, store)
		if err != nil {
			section.Issues = append(section.Issues, flareschema.CollectorIssue{
				Kind:    "config_load_failed",
				Message: fmt.Sprintf("%s: %v", ld.prefix, err),
			})
			continue
		}
		if val == nil {
			continue
		}
		flattenJSON(ld.prefix, val, emit, &section)
	}

	return section
}

// flattenJSON marshals v to JSON and walks the resulting tree,
// emitting dotted-key/value pairs through the emit callback.
func flattenJSON(prefix string, v any, emit func(key, value string), section *flareschema.ConfigSection) {
	raw, err := json.Marshal(v)
	if err != nil {
		section.Issues = append(section.Issues, flareschema.CollectorIssue{
			Kind:    "config_marshal_failed",
			Message: fmt.Sprintf("%s: %v", prefix, err),
		})
		return
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		section.Issues = append(section.Issues, flareschema.CollectorIssue{
			Kind:    "config_unmarshal_failed",
			Message: fmt.Sprintf("%s: %v", prefix, err),
		})
		return
	}
	walkJSON(prefix, decoded, emit)
}

// walkJSON descends an arbitrary json.Unmarshal output and emits dotted-key
// paths. Object keys are visited in alphabetical order for deterministic output.
func walkJSON(prefix string, node any, emit func(key, value string)) {
	switch n := node.(type) {
	case map[string]any:
		keys := make([]string, 0, len(n))
		for k := range n {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			walkJSON(joinKey(prefix, k), n[k], emit)
		}
	case []any:
		for i, child := range n {
			walkJSON(fmt.Sprintf("%s[%d]", prefix, i), child, emit)
		}
	case nil:
		// Skip JSON null.
	case string:
		emit(prefix, n)
	case bool:
		emit(prefix, fmt.Sprintf("%v", n))
	case float64:
		emit(prefix, fmt.Sprintf("%v", n))
	default:
		emit(prefix, fmt.Sprintf("%v", n))
	}
}

func joinKey(prefix, k string) string {
	if prefix == "" {
		return k
	}
	return prefix + "." + k
}

// shouldDropKey returns true for keys that must never appear in a flare.
// Currently drops any key whose lowercased form contains "passcode".
func shouldDropKey(k string) bool {
	lower := strings.ToLower(k)
	return strings.Contains(lower, "passcode")
}

// scrubKeyValue returns "[REDACTED]" when the key matches a secret-like pattern,
// otherwise returns value unchanged. APRS callsigns are explicitly public.
func scrubKeyValue(key, value string) string {
	lower := strings.ToLower(key)
	for _, p := range []string{"password", "secret", "token", "_key", "api_key"} {
		if strings.Contains(lower, p) {
			return "[REDACTED]"
		}
	}
	return value
}
