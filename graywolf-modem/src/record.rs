//! `--record <device> --seconds <N> --out <file.wav>`: capture mono i16 from a
//! cpal input device and write a WAV clip. Reuses audio::soundcard::spawn so
//! recorded samples travel the exact path the modem decodes.

use std::sync::mpsc::sync_channel;
use std::time::{Duration, Instant};

use crate::audio::soundcard::{self, SoundcardConfig};
use crate::audio::CHUNK_QUEUE_DEPTH;
use crate::wavio::write_wav_i16;

pub struct RecordArgs {
    pub device: String,
    pub seconds: u64,
    pub out: String,
    pub sample_rate: u32,
}

pub fn parse_args(args: &[String]) -> Result<RecordArgs, String> {
    let usage =
        "usage: graywolf-modem --record <device> --seconds <N> --out <file.wav> [--rate <hz>]";
    let device = args.first().ok_or_else(|| usage.to_string())?.clone();
    let mut seconds: Option<u64> = None;
    let mut out: Option<String> = None;
    let mut sample_rate: u32 = 48000;
    let mut i = 1;
    while i < args.len() {
        match args[i].as_str() {
            "--seconds" => {
                i += 1;
                seconds = Some(
                    args.get(i)
                        .ok_or_else(|| usage.to_string())?
                        .parse()
                        .map_err(|_| "bad --seconds".to_string())?,
                );
            }
            "--out" => {
                i += 1;
                out = Some(args.get(i).ok_or_else(|| usage.to_string())?.clone());
            }
            "--rate" => {
                i += 1;
                sample_rate = args
                    .get(i)
                    .ok_or_else(|| usage.to_string())?
                    .parse()
                    .map_err(|_| "bad --rate".to_string())?;
            }
            other => return Err(format!("unknown arg: {other}\n{usage}")),
        }
        i += 1;
    }
    Ok(RecordArgs {
        device,
        seconds: seconds.ok_or_else(|| usage.to_string())?,
        out: out.ok_or_else(|| usage.to_string())?,
        sample_rate,
    })
}

/// CLI entry: `--record ...`. Captures `seconds` of audio and writes a WAV.
pub fn run(args: &[String]) -> Result<(), String> {
    let a = parse_args(args)?;
    let (tx, rx) = sync_channel::<crate::audio::AudioChunk>(CHUNK_QUEUE_DEPTH);
    let cfg = SoundcardConfig {
        device_name: a.device.clone(),
        sample_rate: a.sample_rate,
        channels: 1,
        audio_channel: 0,
    };
    let mut source = soundcard::spawn(cfg, tx)?;
    let rate = source.sample_rate;

    let mut samples: Vec<i16> = Vec::new();
    let deadline = Instant::now() + Duration::from_secs(a.seconds);
    while Instant::now() < deadline {
        match rx.recv_timeout(Duration::from_millis(250)) {
            Ok(chunk) => samples.extend_from_slice(&chunk),
            Err(std::sync::mpsc::RecvTimeoutError::Timeout) => continue,
            Err(std::sync::mpsc::RecvTimeoutError::Disconnected) => break,
        }
    }
    source.stop_and_join();

    write_wav_i16(&a.out, &samples, rate)?;
    eprintln!(
        "recorded {} samples ({:.1}s @ {} Hz) -> {}",
        samples.len(),
        samples.len() as f32 / rate as f32,
        rate,
        a.out
    );
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_record_args() {
        let args = vec![
            "plughw:CARD=Device,DEV=0".to_string(),
            "--seconds".to_string(),
            "5".to_string(),
            "--out".to_string(),
            "/tmp/clip.wav".to_string(),
        ];
        let p = parse_args(&args).unwrap();
        assert_eq!(p.device, "plughw:CARD=Device,DEV=0");
        assert_eq!(p.seconds, 5);
        assert_eq!(p.out, "/tmp/clip.wav");
        assert_eq!(p.sample_rate, 48000);
    }

    #[test]
    fn rejects_missing_out() {
        let args = vec!["dev".to_string(), "--seconds".to_string(), "5".to_string()];
        assert!(parse_args(&args).is_err());
    }
}
