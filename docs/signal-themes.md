# Signal themes

Signal Light and Signal Dark replace this feature branch's separate Enhanced
packet view preference. Select either through Preferences → Theme. Graywolf
remains the default, and the other six themes retain their palettes and normal
packet/monitor presentation. Old browser packet-toggle storage is ignored.
The existing theme preference is saved on the server and mirrored in the browser.

## Typography and layout

Inter 4.1 is bundled for interface text and controls. JetBrains Mono 2.304 is
bundled for packet data, code and terminal output, with the existing terminal
Nerd Font fallback. Licenses ship alongside the files. No font CDN is used.

Dashboard/APRS Logs/System Logs use the available width. Settings pages keep
bounded forms. Badge columns grow from 200 px up to 340 px, while addresses
retain a minimum readable width; wrapping is allowed. Timestamp columns size to
the complete date/time, padding and flow accent rather than a fixed pixel width.
On narrow cards the date and clock may occupy separate lines; hours, minutes
and seconds stay together. This also applies to General display sizes (90–200%). Viewers narrower than
880 px use stacked cards. Wide native channel cards place the audio meter,
DCD/RX/TX beside the channel name. The dBFS reading sits left of a bounded
RX audio meter with a -60/-40/-20/0 scale. The meter grows with available
width, normally 180–480 px with a 480 px cap, and stacks below its label/readout
on narrow screens. Multiple beacon buttons
wrap beneath the status row. All per-channel counts and station summary cards
remain available. APRS-IS-only beacon buttons stay on the Beacons page.

KISS-only cards use actual interface state rather than inferring readiness from
native audio devices. Connected, listening, disconnected and unavailable are
separate states. TX readiness also requires governor permission. Audio meters
are omitted; DCD and bad-FCS measurements show unavailable. RX/TX activity is
based on existing frame counters. No simulated-transmit indicator is introduced.
Beacon labels still come from the shared `beaconLabel` helper, including custom
labels when the beacon-label PR is integrated.

Sidebar pinning and draggable packet-log height are implemented independently
in fork PR #9. Signal styles that sidebar but does not duplicate its behavior.
The standard modes and underlying theme selection remain independent of PR #9.

## Final packet-flow palette

These are exact background/accent pairs from `web/src/lib/packetTraffic.js`.
They replace the former toggle's palette when a Signal theme is selected.
Type/origin/encoding badges use a subtle tint derived from the same flow accent;
there is no separate radio-routing or beacon-scheduling color configuration.

| Flow | Light background | Light accent | Dark background | Dark accent |
|---|---|---|---|---|
| RF receive | #E5F4EA | #237A48 | #163A2B | #58D68B |
| APRS-IS receive | #F0F8F2 | #4D7C5B | #254D3C | #ACDBBE |
| RF transmit | #FBE8EB | #B4233B | #48232E | #FF8197 |
| APRS-IS transmit | #FFF1F3 | #A95065 | #54313D | #F4B7C5 |
| RF → IS | #FFF4D6 | #906600 | #443B1E | #E8C967 |
| IS → RF | #FFF0E1 | #A65316 | #492F20 | #F0AD73 |
| Digipeating | #E7F1FC | #236BA8 | #193A55 | #82BDF0 |
| Unknown | #EDF1F5 | #596779 | #293442 | #A6B4C6 |

| Text role | Signal Light | Signal Dark |
|---|---|---|
| Packet payload/body | #182B25 | #F1F5F9 |
| Packet metadata | #43534D | #CBD5E1 |
| Source callsign | #703A12 | #F9D49B |
| Destination | #1C4D75 | #BAE1FA |
| RF path | #613F7A | #DFC9F4 |

## Interface colors

| Role | Signal Light | Signal Dark |
|---|---|---|
| App background | #F3F6F8 | #101720 |
| Panel | #FFFFFF | #17212D |
| Raised surface | #E9EFF3 | #202D3B |
| Border | #CCD6DF | #364657 |
| Main text | #182B25 | #F1F5F9 |
| Secondary text | #4B5D6B | #B9C5D1 |
| Primary action / beacon button | #087F78 | #4DD4C6 |
| Primary button text | #FFFFFF | #082825 |
| Selected navigation background | #087F78 | #126D66 |
| Selected navigation text | #FFFFFF | #FFFFFF |
| Active DCD / RX LED | #237A48 | #58D68B |
| Active TX LED | #B4233B | #FF8197 |
| Audio meter track | #D7E2EA | #34475A |
| Audio meter outline/ticks | #6C8193 | #71869B |

All packet body/metadata/header text pairs are checked for at least 4.5:1
contrast against every flow background. Labels accompany colors. Forced-color
handling uses the system palette. TX records are local events, not proof of
remote reception. Payloads, exports, radio behavior and scheduling are unchanged.

## Acceptance

Check wide and narrow viewers in both themes; native and KISS-only channels;
no/single/multiple enabled RF-capable beacons and long labels; loading/unavailable
KISS state; viewport/font/theme changes; offline fonts and forced colors. Check
switching back to each existing theme, including plain monitor output. Interactive
acceptance is still required before upstream submission.
