// Presentation only. Never infer traffic direction from callsigns, payloads,
// FX.25 metadata or the current channel setting.
export const TRAFFIC_LABELS = Object.freeze({
  'rx-rf': 'RX RF', 'rx-is': 'RX IS', 'tx-rf': 'TX RF', 'tx-is': 'TX IS',
  'rf-is': 'RF → IS', 'is-rf': 'IS → RF', digi: 'DIGI → RF', unknown: 'Unspecified',
});

export function packetFlow(entry = {}) {
  const direction = String(entry.direction || '').toUpperCase();
  const source = String(entry.source || '').toLowerCase();
  const notes = String(entry.notes || '').toLowerCase();
  let id = 'unknown';
  if (direction === 'TX') {
    if (source === 'digipeater' || source === 'digi') id = 'digi';
    else if (source === 'igate' || source === 'igate-tx' || notes === 'is2rf') id = 'is-rf';
    else id = 'tx-rf';
  } else if (direction === 'RX') id = 'rx-rf';
  else if (direction === 'IS') {
    if (notes === 'rf2is') id = 'rf-is';
    else if (source === 'igate-is' || notes === 'is-rx') id = 'rx-is';
    else if (source === 'beacon' || notes === 'aprs-is') id = 'tx-is';
  }
  return { id, label: TRAFFIC_LABELS[id] };
}

// Preserve the complete string, including punctuation, object-name padding,
// non-printable bytes and third-party payloads. Consumers render text safely.
export function packetHeaderSegments(value = '') {
  const text = String(value ?? '');
  const gt = text.indexOf('>'), colon = text.indexOf(':');
  if (gt < 1 || colon <= gt || !/^[A-Za-z0-9-]+$/.test(text.slice(0, gt))) return [{ text, role: 'payload' }];
  const comma = text.indexOf(',', gt + 1);
  const destEnd = comma > gt && comma < colon ? comma : colon;
  const out = [
    { text: text.slice(0, gt), role: 'source' }, { text: '>', role: 'plain' },
    { text: text.slice(gt + 1, destEnd), role: 'destination' },
  ];
  if (destEnd < colon) out.push({ text: ',', role: 'plain' }, { text: text.slice(destEnd + 1, colon), role: 'path' });
  out.push({ text: ':', role: 'plain' }, { text: text.slice(colon + 1), role: 'payload' });
  return out;
}

const IDS = Object.keys(TRAFFIC_LABELS);
const LIGHT = [
  ['#E5F4EA','#237A48'], ['#F0F8F2','#4D7C5B'], ['#FBE8EB','#B4233B'], ['#FFF1F3','#A95065'],
  ['#FFF4D6','#906600'], ['#FFF0E1','#A65316'], ['#E7F1FC','#236BA8'], ['#EDF1F5','#596779'],
];
const DARK = [
  ['#163A2B','#58D68B'], ['#254D3C','#ACDBBE'], ['#48232E','#FF8197'], ['#54313D','#F4B7C5'],
  ['#443B1E','#E8C967'], ['#492F20','#F0AD73'], ['#193A55','#82BDF0'], ['#293442','#A6B4C6'],
];

export function packetPalette(theme = 'graywolf', highContrast = false) {
  const night = theme === 'signal-dark' || theme.endsWith('-night');
  const mono = theme.startsWith('grayscale') || theme.startsWith('high-contrast') || highContrast;
  const contrast = highContrast || theme.startsWith('high-contrast');
  const foreground = mono ? (night ? '#ffffff' : '#000000') : (night ? '#F1F5F9' : '#182B25');
  const background = night ? (contrast ? '#000000' : '#17212D') : '#ffffff';
  const colors = night ? DARK : LIGHT;
  const flows = Object.fromEntries(IDS.map((id, i) => [id, {
    background: mono ? (contrast ? background : (night ? ['#202020','#383838','#242424','#404040','#292929','#353535','#303030','#252525'][i] : ['#e6e6e6','#f4f4f4','#e9e9e9','#f7f7f7','#ececec','#eeeeee','#e0e0e0','#f0f0f0'][i])) : colors[i][0],
    edge: mono ? foreground : colors[i][1],
  }]));
  return { foreground, background, muted: mono ? foreground : (night ? '#CBD5E1' : '#43534D'),
    source: mono ? foreground : (night ? '#F9D49B' : '#703A12'),
    destination: mono ? foreground : (night ? '#BAE1FA' : '#1C4D75'),
    path: mono ? foreground : (night ? '#DFC9F4' : '#613F7A'), flows };
}

export function packetPaletteStyle(palette) {
  const vars = Object.entries(palette).filter(([key]) => key !== 'flows').map(([key,value]) => `--packet-${key}:${value}`);
  for (const [id, colors] of Object.entries(palette.flows)) {
    vars.push(`--packet-${id}-bg:${colors.background}`, `--packet-${id}-edge:${colors.edge}`);
  }
  return vars.join(';');
}
