// Display preference, local to this browser/device like UI scale. Existing
// rendering remains the default; storage denial never prevents using the UI.
import { readPacketColors, savePacketColors } from '../packetColorPreference.js';
export const packetColors = (() => {
  let enabled = $state(readPacketColors());
  function setEnabled(value) {
    enabled = value === true;
    savePacketColors(enabled);
  }
  return { get enabled() { return enabled; }, setEnabled };
})();
