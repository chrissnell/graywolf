package messages

import "strings"

// BotAddress is one well-known APRS service addressee. The autocomplete
// endpoint always surfaces the bot set regardless of whether the
// operator has ever messaged them; tactical-callsign CRUD rejects any
// attempt to register a label colliding with one of these names
// (routing confusion risk).
type BotAddress struct {
	Callsign    string
	Description string
}

// WellKnownBots is the curated list of APRS service addresses. Sourced
// from the APRS101 reference and common bot practice. Add entries as
// new services emerge. Case is canonical uppercase.
var WellKnownBots = []BotAddress{
	{Callsign: "QRX", Description: "Store and forward"},
	{Callsign: "SMS", Description: "Send an SMS via APRS"},
	{Callsign: "FIND", Description: "Locate a callsign"},
	{Callsign: "WHO-IS", Description: "Callsign lookup"},
	{Callsign: "REPEAT", Description: "Message repeater"},
	{Callsign: "WXBOT", Description: "Weather information"},
	{Callsign: "MPAD", Description: "Mobile Position/Address Data"},
	{Callsign: "MAIL", Description: "APRS email gateway"},
	{Callsign: "WLNK-1", Description: "Winlink via APRS"},
}

// BotDirectory is the narrow interface consumed by the autocomplete
// handler. Kept small so tests can inject fakes; production uses
// DefaultBotDirectory.
type BotDirectory interface {
	// List returns every registered bot.
	List() []BotAddress
	// Match returns the subset whose Callsign starts with prefix
	// (case-insensitive). An empty prefix matches everything.
	Match(prefix string) []BotAddress
}

// staticBotDirectory wraps WellKnownBots in the BotDirectory interface.
type staticBotDirectory struct {
	entries []BotAddress
}

func (d *staticBotDirectory) List() []BotAddress {
	out := make([]BotAddress, len(d.entries))
	copy(out, d.entries)
	return out
}

func (d *staticBotDirectory) Match(prefix string) []BotAddress {
	if prefix == "" {
		return d.List()
	}
	up := strings.ToUpper(strings.TrimSpace(prefix))
	var out []BotAddress
	for _, b := range d.entries {
		if strings.HasPrefix(b.Callsign, up) {
			out = append(out, b)
		}
	}
	return out
}

// DefaultBotDirectory is the singleton BotDirectory backed by
// WellKnownBots. Tests may construct their own via NewBotDirectory.
var DefaultBotDirectory BotDirectory = &staticBotDirectory{entries: WellKnownBots}

// NewBotDirectory constructs a BotDirectory over the supplied entries.
// Used by tests that want isolated control of the bot set.
func NewBotDirectory(entries []BotAddress) BotDirectory {
	copied := make([]BotAddress, len(entries))
	copy(copied, entries)
	return &staticBotDirectory{entries: copied}
}

// IsWellKnownBot reports whether callsign (case-insensitive) collides
// with a well-known bot name. The tactical-callsign CRUD handler uses
// this to reject registrations that would poison the routing logic:
// a user who creates a tactical labelled "SMS" would start intercepting
// messages intended for the APRS-SMS bot.
func IsWellKnownBot(callsign string) bool {
	up := strings.ToUpper(strings.TrimSpace(callsign))
	if up == "" {
		return false
	}
	for _, b := range WellKnownBots {
		if b.Callsign == up {
			return true
		}
	}
	return false
}
