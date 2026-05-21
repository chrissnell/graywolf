// Shared formatters for PTT sub-components. Keep tiny — only add when
// multiple components actually need the same logic.

export function truncatePath(p, max = 40) {
  if (!p || p.length <= max) return p || '—';
  return '...' + p.slice(-(max - 3));
}
