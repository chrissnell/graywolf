//! `--decode <file>`: feed a captured clip through the production AfskDemodulator
//! and print a JSON summary (good frames, bad-FCS, per-packet dBFS levels).
//! Deterministic on a fixed clip -> the digital-gain/profile sweep scorer.

use serde::Serialize;

use crate::demod_afsk::AfskDemodulator;
use crate::types::{AfskProfile, DEFAULT_BAUD, DEFAULT_MARK_FREQ, DEFAULT_SPACE_FREQ};
use crate::wavio::{read_audio_i16, to_dbfs};

#[derive(Serialize, Debug, Clone)]
pub struct Summary {
    pub rx_frames: u64,
    pub rx_bad_fcs: u64,
    pub level_dbfs_med: f32,
    pub mark_dbfs_med: f32,
    pub space_dbfs_med: f32,
    pub twist_db_med: f32,
    pub sample_rate: u32,
}

fn median(mut xs: Vec<f32>) -> f32 {
    if xs.is_empty() {
        return -60.0;
    }
    xs.sort_by(|a, b| a.partial_cmp(b).unwrap());
    let n = xs.len();
    if n % 2 == 1 {
        xs[n / 2]
    } else {
        (xs[n / 2 - 1] + xs[n / 2]) / 2.0
    }
}

/// Pure scoring core. `frames` holds (mark, space) linear amplitudes per good
/// frame; `bad_fcs` is the failed-FCS count from the demodulator.
pub fn summarize(frames: &[(f32, f32)], bad_fcs: u64) -> Summary {
    let levels: Vec<f32> = frames
        .iter()
        .map(|(m, s)| to_dbfs((m.max(0.0) + s.max(0.0)) / 2.0))
        .collect();
    let marks: Vec<f32> = frames.iter().map(|(m, _)| to_dbfs(*m)).collect();
    let spaces: Vec<f32> = frames.iter().map(|(_, s)| to_dbfs(*s)).collect();
    let twists: Vec<f32> = marks
        .iter()
        .zip(&spaces)
        .map(|(m, s)| (m - s).abs())
        .collect();
    Summary {
        rx_frames: frames.len() as u64,
        rx_bad_fcs: bad_fcs,
        level_dbfs_med: median(levels),
        mark_dbfs_med: median(marks),
        space_dbfs_med: median(spaces),
        twist_db_med: median(twists),
        sample_rate: 0,
    }
}

/// Decode a clip (.wav or .flac) and return its summary.
pub fn decode_file(path: &str) -> Result<Summary, String> {
    let (samples, rate) = read_audio_i16(path)?;
    // Construct exactly as src/bin/demod_multi.rs::run_cfg does (Profile A, single slicer).
    let mut demod = AfskDemodulator::new(
        rate,
        DEFAULT_BAUD,
        DEFAULT_MARK_FREQ,
        DEFAULT_SPACE_FREQ,
        AfskProfile::A,
        0,
        0,
    );
    for &s in &samples {
        demod.process_sample(s as i32);
    }
    let good = demod.take_frames();
    let bad = demod.take_bad_fcs();
    let pairs: Vec<(f32, f32)> = good
        .iter()
        .map(|f| (f.audio_level_mark, f.audio_level_space))
        .collect();
    let mut summary = summarize(&pairs, bad);
    summary.sample_rate = rate;
    Ok(summary)
}

/// CLI entry: `--decode <file>`.
pub fn run(args: &[String]) -> Result<(), String> {
    let path = args
        .first()
        .ok_or_else(|| "usage: graywolf-modem --decode <file.wav|file.flac>".to_string())?;
    let summary = decode_file(path)?;
    let json = serde_json::to_string_pretty(&summary).map_err(|e| e.to_string())?;
    println!("{json}");
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn summarize_computes_levels_and_counts() {
        let frames = vec![(0.5_f32, 0.5_f32), (0.1_f32, 0.1_f32)];
        let s = summarize(&frames, 3);
        assert_eq!(s.rx_frames, 2);
        assert_eq!(s.rx_bad_fcs, 3);
        // mean(0.5,0.5)=0.5 -> -6.0 ; mean(0.1,0.1)=0.1 -> -20.0 ; median = -13.0
        assert_eq!(s.level_dbfs_med, -13.0);
        assert!((s.twist_db_med - 0.0).abs() < 1e-6);
    }

    #[test]
    fn summarize_handles_no_frames() {
        let s = summarize(&[], 0);
        assert_eq!(s.rx_frames, 0);
        assert_eq!(s.rx_bad_fcs, 0);
    }
}
