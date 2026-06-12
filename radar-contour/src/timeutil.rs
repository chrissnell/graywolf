use anyhow::{anyhow, Result};
use time::format_description::well_known::Rfc3339;
use time::OffsetDateTime;

/// Floor an RFC3339 timestamp to the nearest `window_s` boundary, returning a
/// UTC RFC3339 string with second precision (e.g. 01:07:43 / 300 -> 01:05:00).
pub fn floor_rfc3339(rfc3339: &str, window_s: i64) -> Result<String> {
    let dt = OffsetDateTime::parse(rfc3339, &Rfc3339)
        .map_err(|e| anyhow!("parse rfc3339 {rfc3339:?}: {e}"))?;
    let unix = dt.unix_timestamp();
    let floored = unix - unix.rem_euclid(window_s.max(1));
    let out = OffsetDateTime::from_unix_timestamp(floored)
        .map_err(|e| anyhow!("from_unix_timestamp {floored}: {e}"))?;
    out.format(&Rfc3339).map_err(|e| anyhow!("format: {e}"))
        // time formats UTC as `...+00:00`; canonicalize to `Z`.
        .map(|s| s.replace("+00:00", "Z"))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn floors_to_cadence_window() {
        let id = floor_rfc3339("2026-06-12T01:07:43Z", 300).unwrap();
        assert_eq!(id, "2026-06-12T01:05:00Z");
    }
}
