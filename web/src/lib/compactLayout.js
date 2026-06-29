// Shared "compact layout" breakpoint.
//
// Compact = the small-screen chrome where the full desktop sidebar and
// the map's perma-card layer panel give way to the top bar / drawer /
// FAB. It covers two cases:
//   - portrait phones (narrow width), and
//   - landscape phones (short height) — a phone rotated to landscape is
//     usually wider than 768px, so width alone would drop it back into
//     the desktop layout and the side menu would eat most of the map
//     (GH #419). The orientation+max-height clause keeps landscape phones
//     in the compact chrome while leaving real tablets/laptops (taller
//     viewports) on the full desktop layout.
//
// The exact same conditions are mirrored in CSS (Sidebar.svelte,
// App.svelte, maplibre-map.svelte). Keep the two in sync when changing
// the breakpoint.
export const COMPACT_LAYOUT_QUERY =
  '(max-width: 768px), (orientation: landscape) and (max-height: 500px)';
