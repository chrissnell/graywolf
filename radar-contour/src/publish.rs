use anyhow::{anyhow, Result};
use rusty_s3::{Bucket, Credentials, S3Action};
use serde::{Deserialize, Serialize};
use std::path::Path;
use std::time::Duration;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Latest {
    pub ts: String,
    pub key: String,
    pub minzoom: u8,
    pub maxzoom: u8,
}

/// R2/S3 handle: a request-signing bucket, credentials, and an HTTP client.
pub struct R2Client {
    pub bucket: Bucket,
    pub creds: Credentials,
    pub http: reqwest::Client,
}

/// Presigned-URL validity window. Each request is signed immediately before
/// it is sent, so a short TTL is plenty.
const SIGN_TTL: Duration = Duration::from_secs(900);

pub fn should_skip(current_latest_ts: Option<&str>, new_frame_ts: &str) -> bool {
    current_latest_ts == Some(new_frame_ts)
}

/// Atomic publish: PUT the archive, then overwrite latest.json. A crash
/// between the two leaves a harmless orphan archive, never a half-served frame.
pub async fn publish_frame(
    c: &R2Client, prefix: &str, ts: &str,
    pmtiles_path: &Path, minzoom: u8, maxzoom: u8,
) -> Result<()> {
    let key = format!("{prefix}/{ts}.pmtiles");
    let body = tokio::fs::read(pmtiles_path).await?;
    let url = c.bucket.put_object(Some(&c.creds), &key).sign(SIGN_TTL);
    let resp = c.http.put(url)
        .header("content-type", "application/octet-stream")
        .body(body).send().await?;
    if !resp.status().is_success() {
        return Err(anyhow!("put {key}: {}", resp.status()));
    }

    let latest = Latest { ts: ts.into(), key: key.clone(), minzoom, maxzoom };
    let latest_key = format!("{prefix}/latest.json");
    let url = c.bucket.put_object(Some(&c.creds), &latest_key).sign(SIGN_TTL);
    let resp = c.http.put(url)
        .header("content-type", "application/json")
        .header("cache-control", "no-cache")
        .body(serde_json::to_vec(&latest)?).send().await?;
    if !resp.status().is_success() {
        return Err(anyhow!("put {latest_key}: {}", resp.status()));
    }
    Ok(())
}

/// Read current latest.json. Missing pointer (404) -> Ok(None). ANY OTHER
/// error (network/auth/parse/5xx) propagates, so a transient blip fails the
/// run loudly instead of silently republishing every cycle (review #4).
pub async fn read_latest(c: &R2Client, prefix: &str) -> Result<Option<Latest>> {
    let key = format!("{prefix}/latest.json");
    let url = c.bucket.get_object(Some(&c.creds), &key).sign(SIGN_TTL);
    let resp = c.http.get(url).send().await?;
    if resp.status() == reqwest::StatusCode::NOT_FOUND {
        return Ok(None);
    }
    if !resp.status().is_success() {
        return Err(anyhow!("get {key}: {}", resp.status()));
    }
    let bytes = resp.bytes().await?;
    let latest: Latest = serde_json::from_slice(&bytes)
        .map_err(|e| anyhow!("latest.json parse: {e}"))?;
    Ok(Some(latest))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn latest_json_roundtrips() {
        let l = Latest { ts: "20260612T010203Z".into(),
                         key: "radar/20260612T010203Z.pmtiles".into(),
                         minzoom: 3, maxzoom: 10 };
        let s = serde_json::to_string(&l).unwrap();
        let back: Latest = serde_json::from_str(&s).unwrap();
        assert_eq!(back.ts, l.ts);
        assert_eq!(back.key, l.key);
    }

    #[test]
    fn skips_when_frame_unchanged() {
        assert!(should_skip(Some("frameA"), "frameA"));
        assert!(!should_skip(Some("frameA"), "frameB"));
        assert!(!should_skip(None, "frameA"));
    }
}
