import test from 'node:test';
import assert from 'node:assert/strict';
import { channelAudioPeak, dashboardBeacons, kissChannelStatus, audioMeterPercent, AUDIO_METER_TICKS } from './dashboardPresentation.js';

test('KISS zero-filled audio fields never appear as an audio measurement', () => {
 assert.equal(channelAudioPeak({input_device_id:0,device_peak_dbfs:0,audio_peak:0}),null);
 assert.equal(channelAudioPeak({input_device_id:1,device_peak_dbfs:0,audio_peak:-30}),0);
 assert.equal(channelAudioPeak({input_device_id:1,device_peak_dbfs:-27}),-27);
 assert.equal(channelAudioPeak({input_device_id:1,audio_peak:-40}),-40);
 assert.equal(channelAudioPeak({input_device_id:1,audio_peak:NaN}),null);
});
test('Dashboard keeps all enabled RF-capable beacon buttons and excludes IS-only', () => {
 const beacons=[
  {id:1,enabled:true,channel:2,send_path:'rf',label:'Local RF'},
  {id:2,enabled:true,channel:2,send_path:'both',label:'Tracker'},
  {id:3,enabled:true,channel:2,send_path:'is_only',label:'Internet'},
  {id:4,enabled:false,channel:2,send_path:'rf'},
  {id:5,enabled:true,channel:0,send_path:'is_only'},
 ];
 const grouped=dashboardBeacons(beacons);
 assert.deepEqual(grouped[2],beacons.slice(0,2));
 assert.equal(grouped[0],undefined);
 assert.equal(grouped[2][0].label,'Local RF');
});
test('KISS readiness uses actual TNC state and governor permission', () => {
 const ch={id:2};
 const tnc={enabled:true,mode:'tnc',channel:2,state:'connected',allow_tx_from_governor:true};
 assert.deepEqual(kissChannelStatus(ch,[tnc]),{label:'KISS connected',connected:true,txReady:true});
 assert.equal(kissChannelStatus(ch,[{...tnc,allow_tx_from_governor:false}]).txReady,false);
 assert.equal(kissChannelStatus(ch,[{...tnc,state:'listening'}]).label,'KISS listening');
 assert.equal(kissChannelStatus(ch,[{...tnc,mode:'app'}]).connected,false);
 assert.equal(kissChannelStatus(ch,[{...tnc,channel:3}]).connected,false);
 assert.equal(kissChannelStatus(ch,[{...tnc,enabled:false}]).connected,false);
 assert.equal(kissChannelStatus(ch,[tnc],false).label,'KISS status unavailable');
});

test('audio fill and tick scale share the same -60 to 0 dBFS range', () => {
 assert.deepEqual(AUDIO_METER_TICKS,[-60,-40,-20,0]);
 assert.equal(audioMeterPercent(-60),0);
 assert.equal(audioMeterPercent(0),100);
 assert.equal(audioMeterPercent(-30),50);
 assert.equal(audioMeterPercent(-90),0);
 assert.equal(audioMeterPercent(6),100);
 assert.equal(audioMeterPercent(null),0);
 assert.equal(audioMeterPercent(NaN),0);
 assert.ok(Math.abs(audioMeterPercent(-37)-38.333333)<0.00001);
});
