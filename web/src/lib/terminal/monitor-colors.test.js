import test from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, writeFileSync, unlinkSync } from 'node:fs';
import { compileModule } from 'svelte/compiler';

// Exercise the production rune module with Svelte's server transform: its
// state remains observable, while sockets/DOM are controlled offline fakes.
test('monitor follows theme selection, bounds repaint history and never recounts it',async()=>{
 const file=new URL(`./monitor-test-${process.pid}.tmp.mjs`,import.meta.url);
 const originals=Object.fromEntries(['WebSocket','location','document','window','MutationObserver'].map(k=>[k,globalThis[k]]));
 let socket, observer;
 try {
  globalThis.location={protocol:'http:',host:'test.invalid'};
  globalThis.document={documentElement:{dataset:{theme:'graywolf-night'}}};
  globalThis.window={matchMedia:()=>({matches:false,addEventListener(){},removeEventListener(){}})};
  globalThis.MutationObserver=class {constructor(fn){this.fn=fn;observer=this}observe(){}disconnect(){this.closed=true}};
  globalThis.WebSocket=class {constructor(){socket=this;this.readyState=1}send(){}close(){this.closed=true}};
  const source=readFileSync(new URL('./monitor.svelte.js',import.meta.url),'utf8');
  writeFileSync(file,compileModule(source,{generate:'server',filename:'monitor.svelte.js'}).js.code);
  const {createMonitorSession}=await import(file.href);
  const monitor=createMonitorSession();const output=[];
  monitor.state.onDataRX=bytes=>output.push(new TextDecoder().decode(bytes));
  for(let i=0;i<510;i++)socket.onmessage({data:JSON.stringify({kind:'raw_tail',raw_tail:{raw:`A>B:packet-${i}`,direction:'IS',source:'igate',notes:'rf2is'}})});
  assert.equal(output[0],'A>B:packet-0\r\n');assert.equal(monitor.state.lineCount,510);
  output.length=0;globalThis.document.documentElement.dataset.theme='signal-dark';observer.fn();
  assert.equal(output.length,501);assert.ok(output[1].includes('RF → IS'));
  assert.ok(output[1].includes('packet-10'));assert.ok(output.at(-1).includes('packet-509'));
  assert.equal(monitor.state.lineCount,510);
  output.length=0;globalThis.document.documentElement.dataset.theme='signal-light';observer.fn();
  assert.equal(output.length,501);assert.equal(monitor.state.lineCount,510);
  output.length=0;globalThis.document.documentElement.dataset.theme='graywolf';observer.fn();
  assert.equal(output[1],'A>B:packet-10\r\n');assert.equal(output.at(-1),'A>B:packet-509\r\n');
  output.length=0;monitor.clearScreen();globalThis.document.documentElement.dataset.theme='signal-dark';observer.fn();
  assert.equal(output.length,2); // clear + repaint clear; no old packet restored
  monitor.close();assert.equal(socket.closed,true);assert.equal(observer.closed,true);
 } finally {
  try{unlinkSync(file)}catch{}
  for(const [key,value] of Object.entries(originals)){if(value===undefined)delete globalThis[key];else globalThis[key]=value;}
 }
});
