// Package docs holds the frozen operation-ID registry for the webapi
// package's OpenAPI spec.
//
// DO NOT RENAME constants in this file.
//
// Every @ID value used in a swag annotation block under pkg/webapi/**
// MUST be the string literal value of one of the constants declared
// here. This file is the source of truth:
//
//   - Generated clients (TypeScript, Rust, Python, …) use the
//     operationId as the generated method name, so renaming a value
//     here is a wire-breaking change that ripples into every consumer.
//     Add new IDs rather than renaming existing ones. If deprecation is
//     required, keep the old constant (optionally marked with a comment)
//     and introduce a new one alongside it.
//   - Swag (v1.x) does not interpolate Go constants into annotation
//     strings, so the @ID literal in the handler doc block is duplicated
//     text. The `make docs-lint` target greps @ID values across
//     pkg/webapi and pkg/webauth and asserts each one appears as a
//     constant value in this file. Keep the two in sync.
//
// IDs follow lowerCamelCase so the generated client method names are
// idiomatic in every target language. Group constants by resource.
package docs

// Channels resource — /api/channels (Phase 1 reference migration).
const (
	OpListChannels        = "listChannels"
	OpCreateChannel       = "createChannel"
	OpGetChannel          = "getChannel"
	OpUpdateChannel       = "updateChannel"
	OpDeleteChannel       = "deleteChannel"
	OpGetChannelStats     = "getChannelStats"
	OpGetChannelReferrers = "getChannelReferrers"
)

// Beacons resource — /api/beacons (Phase 2).
const (
	OpListBeacons  = "listBeacons"
	OpCreateBeacon = "createBeacon"
	OpGetBeacon    = "getBeacon"
	OpUpdateBeacon = "updateBeacon"
	OpDeleteBeacon = "deleteBeacon"
	OpSendBeacon   = "sendBeacon"
)

// SmartBeacon config — singleton at /api/smart-beacon. Tagged under
// the `beacons` group so the Swagger UI clusters it with the
// per-beacon CRUD endpoints. The curve parameters are global (no
// per-beacon override), mirroring direwolf's SMARTBEACON directive.
const (
	OpGetSmartBeacon    = "getSmartBeacon"
	OpUpdateSmartBeacon = "updateSmartBeacon"
)

// Audio devices resource — /api/audio-devices (Phase 2).
//
// Sub-resource endpoints (available, scan-levels, levels, test-tone,
// gain) stay under the same tag. Operation IDs follow the
// verbResource convention so generated clients read fluently
// (client.listAvailableAudioDevices(), client.setAudioDeviceGain(...)).
const (
	OpListAudioDevices          = "listAudioDevices"
	OpCreateAudioDevice         = "createAudioDevice"
	OpGetAudioDevice            = "getAudioDevice"
	OpUpdateAudioDevice         = "updateAudioDevice"
	OpDeleteAudioDevice         = "deleteAudioDevice"
	OpListAvailableAudioDevices = "listAvailableAudioDevices"
	OpScanAudioDeviceLevels     = "scanAudioDeviceLevels"
	OpGetAudioDeviceLevels      = "getAudioDeviceLevels"
	OpPlayTestTone              = "playTestTone"
	OpSetAudioDeviceGain        = "setAudioDeviceGain"
)

// KISS interfaces resource — /api/kiss (Phase 2).
const (
	OpListKiss      = "listKiss"
	OpCreateKiss    = "createKiss"
	OpGetKiss       = "getKiss"
	OpUpdateKiss    = "updateKiss"
	OpDeleteKiss    = "deleteKiss"
	OpReconnectKiss = "reconnectKiss"
)

// Tx-timing resource — /api/tx-timing (Phase 2). Keyed by channel id,
// upsert semantics — no delete op.
const (
	OpListTxTiming   = "listTxTiming"
	OpCreateTxTiming = "createTxTiming"
	OpGetTxTiming    = "getTxTiming"
	OpUpdateTxTiming = "updateTxTiming"
)

// Digipeater rules resource — /api/digipeater/rules (Phase 2). The
// singleton config at /api/digipeater is Phase 3's concern and not
// registered here. No single-rule GET exists — the list endpoint
// returns all rules and the UI filters client-side.
const (
	OpListDigipeaterRules  = "listDigipeaterRules"
	OpCreateDigipeaterRule = "createDigipeaterRule"
	OpUpdateDigipeaterRule = "updateDigipeaterRule"
	OpDeleteDigipeaterRule = "deleteDigipeaterRule"
)

// Igate RF filters resource — /api/igate/filters (Phase 2). The
// singleton config at /api/igate/config is Phase 3's concern — see
// the Phase 3 block below.
const (
	OpListIgateFilters  = "listIgateFilters"
	OpCreateIgateFilter = "createIgateFilter"
	OpUpdateIgateFilter = "updateIgateFilter"
	OpDeleteIgateFilter = "deleteIgateFilter"
)

// Singleton config resources and small near-singletons (Phase 3).
//
// These sit alongside the Phase 2 CRUD blocks for digipeater rules
// and igate filters; the split is by shape (singleton PUT-upsert
// config) not by URL tree.

// Digipeater config — singleton at /api/digipeater.
const (
	OpGetDigipeaterConfig    = "getDigipeaterConfig"
	OpUpdateDigipeaterConfig = "updateDigipeaterConfig"
)

// Igate config — singleton at /api/igate/config.
const (
	OpGetIgateConfig    = "getIgateConfig"
	OpUpdateIgateConfig = "updateIgateConfig"
)

// GPS resource — singleton at /api/gps plus a small list endpoint at
// /api/gps/available for the serial-port picker.
const (
	OpGetGps           = "getGps"
	OpUpdateGps        = "updateGps"
	OpListAvailableGps = "listAvailableGps"
)

// Position-log resource — singleton at /api/position-log.
const (
	OpGetPositionLog    = "getPositionLog"
	OpUpdatePositionLog = "updatePositionLog"
)

// AGW resource — singleton at /api/agw.
const (
	OpGetAgw    = "getAgw"
	OpUpdateAgw = "updateAgw"
)

// Health and status — single-handler, GET-only, no path params.
const (
	OpGetHealth = "getHealth"
	OpGetStatus = "getStatus"
)

// Out-of-band resources — routes installed by RegisterXxx helpers or
// by pkg/app/wiring.go rather than by Server.RegisterRoutes (Phase 5).

// Igate status/simulation — /api/igate, /api/igate/simulation. The
// singleton config and filters live under Phase 2/3 blocks above.
const (
	OpGetIgateStatus     = "getIgateStatus"
	OpSetIgateSimulation = "setIgateSimulation"
)

// Packets — /api/packets.
const (
	OpListPackets = "listPackets"
)

// Position — /api/position.
const (
	OpGetPosition = "getPosition"
)

// Stations — /api/stations.
const (
	OpListStations = "listStations"
)

// Version — /api/version. The handler lives in pkg/webapi/version.go
// and is installed via webapi.RegisterVersion; wiring.go mounts it on
// the outer (public) mux.
const (
	OpGetVersion = "getVersion"
)

// Auth — /api/auth/login, /api/auth/logout, /api/auth/setup. Handlers
// live in pkg/webauth/handlers.go and are registered by pkg/app/wiring.go
// onto the outer (public) mux, not the RequireAuth-wrapped apiMux.
const (
	OpLogin           = "login"
	OpLogout          = "logout"
	OpGetSetupStatus  = "getSetupStatus"
	OpCreateFirstUser = "createFirstUser"
)

// Messages resource — /api/messages (APRS messaging feature).
//
// The messages surface covers DM + tactical-broadcast inbound/outbound
// rows, preferences, tactical callsign CRUD, conversation rollup,
// per-thread participants, and an SSE event stream. The autocomplete
// endpoint is an out-of-band helper registered via
// webapi.RegisterStationsAutocomplete; its operation ID lives alongside
// the messages block because it's the same feature slice, even though
// the URL sits under /api/stations.
const (
	OpListMessages            = "listMessages"
	OpGetMessage              = "getMessage"
	OpSendMessage             = "sendMessage"
	OpDeleteMessage           = "deleteMessage"
	OpMarkMessageRead         = "markMessageRead"
	OpMarkMessageUnread       = "markMessageUnread"
	OpResendMessage           = "resendMessage"
	OpListConversations       = "listConversations"
	OpStreamMessageEvents     = "streamMessageEvents"
	OpGetMessagePreferences   = "getMessagePreferences"
	OpPutMessagePreferences   = "putMessagePreferences"
	OpListTacticalCallsigns   = "listTacticalCallsigns"
	OpCreateTacticalCallsign  = "createTacticalCallsign"
	OpUpdateTacticalCallsign  = "updateTacticalCallsign"
	OpDeleteTacticalCallsign  = "deleteTacticalCallsign"
	OpGetTacticalParticipants = "getTacticalParticipants"
	OpAcceptTacticalInvite    = "acceptTacticalInvite"
	OpAutocompleteStations    = "autocompleteStations"
)

// PTT resource — /api/ptt (Phase 4).
//
// Breaking change versus pre-Phase-4: the GPIO-lines endpoint moved
// from a query-string form (/api/ptt/gpio-lines?chip=/dev/gpiochipN)
// to a path-parameter form (/api/ptt/gpio-chips/{chip}/lines) where
// {chip} is the URL-encoded device path. The operationId reflects the
// new URL shape.
const (
	OpListPttConfigs     = "listPttConfigs"
	OpUpsertPttConfig    = "upsertPttConfig"
	OpListPttDevices     = "listPttDevices"
	OpGetPttCapabilities = "getPttCapabilities"
	OpTestRigctld        = "testRigctld"
	OpListGpioLines      = "listGpioLines"
	OpGetPttConfig       = "getPttConfig"
	OpUpdatePttConfig    = "updatePttConfig"
	OpDeletePttConfig    = "deletePttConfig"
)
