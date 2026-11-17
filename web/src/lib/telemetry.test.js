import test from 'node:test';
import assert from 'node:assert/strict';
import { telemetryLabels, telemetryRows, telemetryState, telemetryDeliveryError } from './telemetry.js';
test('telemetry preserves five-slot order and valid zero counts', () => {
 assert.deepEqual(telemetryLabels, ['RF Rx','Digi','RF Tx','RF → IS','IS → RF']);
 const rows = telemetryRows({ready:true, counts:[0,1,376,3,4], encoded:[0,1,255,3,4]});
 assert.equal(rows[0].count,0);assert.equal(rows[2].count,376);assert.equal(rows[2].encoded,255);
});
test('unavailable and incomplete measurements are not presented as zeros', () => {
 assert.equal(telemetryRows(null)[0].count,'—');
 assert.equal(telemetryRows({ready:false,counts:[0,0,0,0,0]})[0].count,'—');
 assert.equal(telemetryState(null),'Status unavailable');
 assert.match(telemetryState({state:'warming_up',warmup_seconds:599}),/10 min/);
});

test('RF delivery modes require a selected channel before saving', () => {
 for (const delivery of ['auto','rf']) {
  assert.match(telemetryDeliveryError(delivery, '0'), /RF delivery channel/);
  assert.equal(telemetryDeliveryError(delivery, '1'), '');
 }
 assert.equal(telemetryDeliveryError('aprsis', '0'), '');
});
