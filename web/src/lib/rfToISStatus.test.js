import test from 'node:test';
import assert from 'node:assert/strict';
import {rfToISStatus as status} from './rfToISStatus.js';
test('master off overrides connectivity and simulation', () => {
 for (const connected of [true,false]) for (const simulation_mode of [true,false])
  assert.equal(status({rf_to_is_enabled:false,connected,simulation_mode}).tone, 'disabled');
});
test('active, disconnected and simulation states follow runtime status', () => {
 assert.equal(status({rf_to_is_enabled:true,connected:true}).tone,'active');
 assert.equal(status({rf_to_is_enabled:true,connected:false}).tone,'pending');
 assert.equal(status({rf_to_is_enabled:true,connected:true,simulation_mode:true}).tone,'simulation');
});
test('unavailable and older API responses never look active', () => {
 assert.equal(status({connected:true}).tone,'unknown');
 assert.equal(status({rf_to_is_enabled:true,connected:true},false).tone,'unknown');
 assert.equal(status(null).tone,'disabled');
});
test('live disable and re-enable', () => {
 for (const enabled of [true,false,true]) assert.equal(status({rf_to_is_enabled:enabled,connected:true}).tone,enabled?'active':'disabled');
});
