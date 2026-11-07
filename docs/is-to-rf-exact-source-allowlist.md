# APRS-IS → RF: allow a specific non-message source

Normally, Graywolf permits non-message APRS-IS packets to reach RF only
when their source shares the iGate station's base callsign, excluding
the iGate's own exact transmitting callsign.

To opt in an additional source, open **iGate → APRS-IS Feed & TX Rules**
and add a rule:

- **Type:** Callsign
- **Pattern:** the exact APRS packet source (for example, `N6110L`)
- **Action:** Allow
- **Enabled:** on
- **Priority:** ahead of any broader matching rule

An exact callsign match is case-insensitive. It is **not** a wildcard:
`KS9N` does not match `KS9N-7`; add separate exact rules for
individual SSIDs as needed. A Prefix or Packet Type allow rule alone
does **not** authorize an unrelated non-message source.

Also ensure the APRS-IS **server filter** actually delivers that
source's packets, **Enable IS→RF gating** is on, and a TX-capable channel
is selected. The server filter only controls reception; it does not
authorize RF transmission.

Safety checks remain in force: the first matching transmit rule wins
(default deny), the iGate's own exact source is never retransmitted,
packets whose path already contains the iGate are rejected, and
third-party wrapping and TX-governor constraints still apply.
Directed-message heard-direct restrictions are unchanged.

This feature is opt-in via an existing exact Callsign allow rule. It
does not add a global bypass, and it does not implement a new
packet-level duplicate cache. Test with simulation mode and an
appropriately narrow APRS-IS server filter before transmitting.

The first matching rule must be an exact Callsign Allow rule for an
unrelated non-message source; an earlier broad Allow rule does not grant
this exception. This keeps the opt-in narrowly scoped.
