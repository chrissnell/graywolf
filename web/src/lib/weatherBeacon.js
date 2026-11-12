export function validateWeatherBeacon(form) {
  if (form?.type !== 'weather') return '';
  if (form.weather_source === 'wxnow_file') {
    if (typeof form.weather_path !== 'string' || form.weather_path.trim() === '') {
      return 'WxNow.txt path is required for a weather beacon';
    }
    return '';
  }
  if (form.weather_source === 'davis_serial') {
    if (typeof form.weather_device !== 'string' || form.weather_device.trim() === '') {
      return 'Serial device is required for a Davis weather beacon';
    }
    if (!Number.isInteger(Number(form.weather_baud)) || Number(form.weather_baud) <= 0) {
      return 'Serial baud must be a positive integer';
    }
    if (form.weather_bucket !== '0.2mm' && form.weather_bucket !== '0.01in') {
      return 'Select the Davis rain collector size';
    }
    return '';
  }
  if (form.weather_source === 'peet_serial') {
    if (typeof form.weather_device !== 'string' || form.weather_device.trim() === '') {
      return 'Serial device is required for a Peet Bros weather beacon';
    }
    if (!Number.isInteger(Number(form.weather_baud)) || Number(form.weather_baud) <= 0) {
      return 'Serial baud must be a positive integer';
    }
    if (form.weather_bucket !== '0.1mm' && form.weather_bucket !== '0.01in') {
      return 'Select the Peet Bros rain counter unit';
    }
    return '';
  }
  return 'Select a supported weather source';
}
