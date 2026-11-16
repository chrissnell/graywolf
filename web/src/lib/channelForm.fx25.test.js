import test from 'node:test';
import assert from 'node:assert/strict';
import { blankForm, rowToForm, formToPayload, validateForm } from './channelForm.js';
test('FX.25 defaults off and survives edits', () => {
  assert.equal(blankForm().fx25_encode, false);
  const form = rowToForm({ ...formToPayload({...blankForm(), input_device_id:'1'}), fx25_encode:true });
  assert.equal(formToPayload(form).fx25_encode, true);
  assert.equal(validateForm(form).fx25_encode, undefined);
});
test('FX.25 rejects unsupported rates, tones and KISS channels', () => {
  const form={...blankForm(),input_device_id:'1',fx25_encode:true};
  for (const bad of [{bit_rate:'300'},{modem_type:'psk'},{mark_freq:'1600'},{space_freq:'1800'},{channel_type:'kiss-tnc'},{il2p_encode:true}]) {
    assert.ok(validateForm({...form,...bad}).fx25_encode);
  }
});
test('FX.25 public modes are explicit, retained while disabled', () => {
  const f={...blankForm(),input_device_id:'1',fx25_encode:false,fx25_mode:'64'};
  assert.equal(formToPayload(f).fx25_mode,'64');
  assert.equal(formToPayload(f).fx25_encode,false);
  assert.equal(rowToForm(formToPayload(f)).fx25_mode,'64');
  assert.equal(validateForm({...f,fx25_mode:'0'}).fx25_encode,'Invalid FX.25 parity selection.');
});
