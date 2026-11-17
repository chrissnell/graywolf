import test from 'node:test';
import assert from 'node:assert/strict';
import { packetFlow, packetHeaderSegments, packetPalette, packetPaletteStyle } from './packetTraffic.js';
import { formatMonitorPacket } from './packetMonitorColors.js';

test('flow uses event context, with digi and gating before generic TX',()=>{
 const cases=[
  [{direction:'RX',source:'modem'},'rx-rf'], [{direction:'RX',source:'kiss'},'rx-rf'],
  [{direction:'IS',source:'igate-is',notes:'is-rx'},'rx-is'],
  [{direction:'TX',source:'beacon'},'tx-rf'], [{direction:'TX',source:'digipeater'},'digi'],
  [{direction:'TX',source:'igate',notes:'is2rf'},'is-rf'],
  [{direction:'IS',source:'igate',notes:'rf2is'},'rf-is'],
  [{direction:'IS',source:'beacon',notes:'aprs-is'},'tx-is'],
  [{direction:'IS'},'unknown'], [{source:'modem',fx25:{tag:6}},'unknown'], [{},'unknown'],
 ];
 for(const [entry,id] of cases)assert.equal(packetFlow(entry).id,id);
});

test('header segments preserve payload bytes, padding, controls and literal HTML',()=>{
 for(const s of ['A>B:;PARK     *data','A>B,C*,D:}X>Y:payload:more','A>B:\x00\x7f <b>&hello','not a TNC2 packet','','A>B:é']){
  assert.equal(packetHeaderSegments(s).map(x=>x.text).join(''),s);
 }
 const parts=packetHeaderSegments('A>B,C*,D:payload:more');
 assert.equal(parts.find(x=>x.role==='path').text,'C*,D');
 assert.equal(parts.at(-1).text,'payload:more');
});

test('disabled monitor is byte-for-byte the legacy plain formatter',()=>{
 const s='A>B:;PARK     *<hello>\x7f';
 assert.equal(formatMonitorPacket({raw:s},false),s+'\r\n');
 assert.equal(formatMonitorPacket({raw:'x'.repeat(2000)},false),'x'.repeat(1024)+'\r\n');
});

test('colored monitor cannot execute escape sequences supplied in a packet',()=>{
 const out=formatMonitorPacket({direction:'TX',source:'digipeater',raw:'A>B:evil\x1b[2J\ntext'},true,packetPalette('signal-dark'));
 assert.ok(out.includes('DIGI → RF'));assert.ok(!out.includes('evil\x1b[2J'));
 assert.ok(out.includes('evil?[2J?text'));assert.ok(out.endsWith('\x1b[K\x1b[0m\r\n'));
});

function luminance(hex){return [1,3,5].map(i=>parseInt(hex.slice(i,i+2),16)/255).map(v=>v<=.04045?v/12.92:((v+.055)/1.055)**2.4).reduce((s,v,i)=>s+v*[.2126,.7152,.0722][i],0);}
function ratio(a,b){const [lo,hi]=[luminance(a),luminance(b)].sort((x,y)=>x-y);return (hi+.05)/(lo+.05);}
test('every shipped theme keeps packet body, metadata and header text readable',()=>{
 for(const theme of ['signal-light','signal-dark']){
  const p=packetPalette(theme);
  assert.ok(packetPaletteStyle(p).includes('--packet-rx-rf-bg:'));
  for(const flow of Object.values(p.flows)) for(const key of ['foreground','muted','source','destination','path']){
   assert.ok(ratio(flow.background,p[key])>=4.5,`${theme} ${key}: ${ratio(flow.background,p[key])}`);
  }
 }
});

import { isSignalTheme } from './themes/registry.js';
test('only Signal themes enable enhanced packets; legacy themes remain unchanged', () => {
 for (const theme of ['graywolf','graywolf-night','grayscale','grayscale-night','high-contrast','high-contrast-night','unicorn',null]) assert.equal(isSignalTheme(theme),false);
 assert.equal(isSignalTheme('signal-light'),true);
 assert.equal(isSignalTheme('signal-dark'),true);
});

test('forced contrast retains labels while using a monochrome packet palette', () => {
 for(const theme of ['signal-light','signal-dark']) {
  const p=packetPalette(theme,true);
  for(const flow of Object.values(p.flows)) {
   assert.ok(ratio(flow.background,p.foreground)>=7);
   assert.equal(flow.edge,p.foreground);
  }
 }
});

import { readFileSync } from 'node:fs';
test('Signal interface text and primary buttons meet contrast targets', () => {
 for(const id of ['signal-light','signal-dark']) {
  const css=readFileSync(new URL(`../../themes/${id}.css`,import.meta.url),'utf8');
  const colors=Object.fromEntries([...css.matchAll(/--color-([\w-]+):\s*(#[0-9A-Fa-f]{6})/g)].map(m=>[m[1],m[2]]));
  for(const surface of ['bg','surface','surface-raised']) for(const role of ['text','text-muted','text-dim']) {
   assert.ok(ratio(colors[surface],colors[role])>=4.5,`${id} ${surface}/${role}`);
  }
  assert.ok(ratio(colors.primary,colors['primary-fg'])>=4.5,`${id} primary button`);
  const palette=packetPalette(id);
  for(const flow of Object.values(palette.flows)) {
   const mixed='#'+[1,3,5].map(i=>Math.round(parseInt(flow.background.slice(i,i+2),16)*.84+parseInt(flow.edge.slice(i,i+2),16)*.16).toString(16).padStart(2,'0')).join('');
   assert.ok(ratio(mixed,palette.foreground)>=4.5,`${id} badge text`);
  }
 }
});
