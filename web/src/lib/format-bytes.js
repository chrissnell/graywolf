// IEC binary prefix formatter. Matches what `du -h` shows on Linux/macOS.
// Used by the downloads UI for size readouts and progress text.
const UNITS = ['bytes', 'KiB', 'MiB', 'GiB', 'TiB'];

export function formatBytes(n) {
  if (!Number.isFinite(n) || n <= 0) return '0 bytes';
  const idx = Math.min(UNITS.length - 1, Math.floor(Math.log(n) / Math.log(1024)));
  const v = n / Math.pow(1024, idx);
  return `${v < 10 ? v.toFixed(1) : Math.round(v)} ${UNITS[idx]}`;
}
