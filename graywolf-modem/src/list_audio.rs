//! `--list-audio` subcommand: emit cpal host/device topology as JSON
//! matching `graywolf/pkg/flareschema.AudioDevices`. Used by the future
//! `graywolf flare` CLI to capture the audio stack from the same crate
//! the modem uses at runtime, so reports never disagree with what the
//! modem actually sees.

use crate::audio::soundcard::listing;

/// Run the enumeration and return the JSON string. Errors are baked
/// into the AudioDevices.issues field, never returned out — `--list-*`
/// flags must always emit a parseable document.
pub fn run() -> String {
    let result = listing::enumerate();
    serde_json::to_string(&result).unwrap_or_else(|e| {
        // Catastrophic only if AudioDevices itself becomes unserializable;
        // emit a minimal valid document so the Go side's Unmarshal still
        // succeeds and the issue lands in the issues array of an empty
        // enumeration.
        format!(
            "{{\"hosts\":[],\"issues\":[{{\"kind\":\"json_marshal_failed\",\"message\":{:?}}}]}}",
            e.to_string()
        )
    })
}
