import test from 'node:test';
import assert from 'node:assert/strict';
import { validateWeatherBeacon } from './weatherBeacon.js';

test('weather source settings validate before save', () => {
  assert.equal(validateWeatherBeacon({type: 'position'}), '');
  assert.match(validateWeatherBeacon({type: 'weather'}), /supported/);
  for (const source of ['wxnow_file', 'davis_serial', 'peet_serial']) {
    const form = {type: 'weather', weather_source: source, weather_path: '/weather/WxNow.txt',
      weather_device: '/dev/ttyUSB0', weather_baud: source === 'peet_serial' ? '2400' : '19200',
      weather_bucket: source === 'peet_serial' ? '0.01in' : '0.2mm'};
    assert.equal(validateWeatherBeacon(form), '');
    if (source === 'wxnow_file') {
      assert.match(validateWeatherBeacon({...form, weather_path: ' '}), /path/);
    } else {
      assert.match(validateWeatherBeacon({...form, weather_device: ' '}), /device/);
      assert.match(validateWeatherBeacon({...form, weather_baud: '0'}), /positive integer/);
      assert.match(validateWeatherBeacon({...form, weather_bucket: 'invalid'}), /rain/);
    }
  }
});
