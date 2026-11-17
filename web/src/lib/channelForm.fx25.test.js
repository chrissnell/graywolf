import test from 'node:test';
import assert from 'node:assert/strict';
import { blankForm, rowToForm, formToPayload, validateForm, experimentalFx25TxNeedsConfirmation, FX25_300_WARNING } from './channelForm.js';
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

test('experimental 300-baud FX.25 supports correct tones, defaults off and confirms opt-in', () => {
  const hf = {...blankForm(), name:'HF test', input_device_id:'1', bit_rate:'300', mark_freq:'1600', space_freq:'1800'};
  assert.equal(hf.fx25_encode, false);
  assert.equal(validateForm({...hf, fx25_encode:true}).fx25_encode, undefined);
  assert.equal(experimentalFx25TxNeedsConfirmation(hf, null), false);
  hf.fx25_encode = true;
  assert.equal(experimentalFx25TxNeedsConfirmation(hf, null), true);
  const saved = formToPayload(hf);
  assert.equal(experimentalFx25TxNeedsConfirmation(hf, saved), false);
  assert.equal(experimentalFx25TxNeedsConfirmation({...hf, fx25_mode:'64'}, saved), true);
  assert.equal(experimentalFx25TxNeedsConfirmation({...hf, output_device_id:'2'}, saved), true);
  assert.equal(experimentalFx25TxNeedsConfirmation(hf, {...saved, bit_rate:1200}), true);
  assert.equal(experimentalFx25TxNeedsConfirmation({...hf,fx25_encode:false}, saved), false);
  assert.equal(experimentalFx25TxNeedsConfirmation({...hf,bit_rate:'1200'}, null), false);
  assert.match(FX25_300_WARNING,/0.64\/1.07\/1.92/);
});
