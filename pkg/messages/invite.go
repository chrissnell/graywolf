package messages

import "regexp"

// inviteWireRe is the strict wire format for a tactical-invite APRS
// message body: `!GW1 INVITE <TAC>`. The `!GW1` sigil reserves a
// versioned namespace for future verbs (LEAVE, notes, multi-tactical)
// and makes the framing unambiguous versus a casual DM that happens to
// contain the word "INVITE".
//
// Strictness is intentional:
//   - Anchored at both ends (`^...$`): no trailing text, no leading
//     whitespace. Whitespace-tolerance invites footguns where a wire
//     corruption paints a normal DM as an invite.
//   - Uppercase only. APRS addressee casing is normalized by the router
//     already, but we apply the same rule to the tactical token here so
//     `!gw1 invite tac-net` is rejected at persist time rather than
//     silently upcased.
//   - Tactical character class matches TacticalCallsign.Callsign: 1-9
//     of [A-Z0-9-].
//
// Keep this regex identical to the plan's §"Wire protocol" anchor; the
// inbound detector and any future outbound wire-builder must agree on
// exactly one shape.
var inviteWireRe = regexp.MustCompile(`^!GW1 INVITE ([A-Z0-9-]{1,9})$`)

// ParseInvite returns the referenced tactical callsign and ok=true iff
// text is a valid invite wire body per the strict grammar above. It is
// deliberately side-effect-free: the caller (Router.persistInbound)
// decides what to do with the result (stamp Kind=invite + InviteTactical
// on the Message row, or fall through to plain text).
//
// ParseInvite does NOT lowercase, trim, or normalize text — the APRS
// body is persisted verbatim and we classify on exactly what was
// received. A legacy `INVITE TAC` body (no sigil) returns ok=false and
// persists as plain text.
func ParseInvite(text string) (tactical string, ok bool) {
	m := inviteWireRe.FindStringSubmatch(text)
	if m == nil {
		return "", false
	}
	return m[1], true
}
