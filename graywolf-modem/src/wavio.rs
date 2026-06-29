//! WAV/FLAC sample I/O and the dBFS helper shared by the record/decode
//! subcommands. WAV uses `hound`; FLAC reuses `claxon` (already a dep).

use std::path::Path;

/// Linear amplitude (1.0 = full scale) to dBFS, matching the Go `toDBFS`
/// (`pkg/app/rxfanout.go`) and the device meter: non-positive -> -60,
/// otherwise 20*log10(v) floored at -60, rounded to one decimal.
pub fn to_dbfs(v: f32) -> f32 {
    if v <= 0.0 {
        return -60.0;
    }
    let db = (20.0 * v.log10()).max(-60.0);
    let rounded = (db * 10.0).round() / 10.0;
    // Normalize -0.0 -> 0.0 so JSON emits "0", matching the Go toDBFS (a level
    // just under full scale rounds to a negative zero otherwise).
    if rounded == 0.0 {
        0.0
    } else {
        rounded
    }
}

/// Write mono i16 samples as a PCM s16le WAV at `rate` Hz.
pub fn write_wav_i16(path: &str, samples: &[i16], rate: u32) -> Result<(), String> {
    let spec = hound::WavSpec {
        channels: 1,
        sample_rate: rate,
        bits_per_sample: 16,
        sample_format: hound::SampleFormat::Int,
    };
    let mut w = hound::WavWriter::create(path, spec).map_err(|e| e.to_string())?;
    for &s in samples {
        w.write_sample(s).map_err(|e| e.to_string())?;
    }
    w.finalize().map_err(|e| e.to_string())
}

/// Read a WAV (.wav) or FLAC (.flac) file into mono i16 samples. For
/// multi-channel input, channel 0 is taken. Returns (samples, sample_rate).
pub fn read_audio_i16(path: &str) -> Result<(Vec<i16>, u32), String> {
    let ext = Path::new(path)
        .extension()
        .and_then(|e| e.to_str())
        .unwrap_or("")
        .to_ascii_lowercase();
    match ext.as_str() {
        "flac" => read_flac_i16(path),
        _ => read_wav_i16(path),
    }
}

fn read_wav_i16(path: &str) -> Result<(Vec<i16>, u32), String> {
    let mut r = hound::WavReader::open(path).map_err(|e| e.to_string())?;
    let spec = r.spec();
    let ch = spec.channels.max(1) as usize;
    let mut out = Vec::new();
    match spec.sample_format {
        hound::SampleFormat::Int => {
            let shift = 16i32 - spec.bits_per_sample as i32;
            for (i, s) in r.samples::<i32>().enumerate() {
                let s = s.map_err(|e| e.to_string())?;
                if i % ch == 0 {
                    let v = if shift > 0 { s << shift } else { s >> (-shift) };
                    out.push(v.clamp(i16::MIN as i32, i16::MAX as i32) as i16);
                }
            }
        }
        hound::SampleFormat::Float => {
            for (i, s) in r.samples::<f32>().enumerate() {
                let s = s.map_err(|e| e.to_string())?;
                if i % ch == 0 {
                    out.push((s.clamp(-1.0, 1.0) * 32767.0) as i16);
                }
            }
        }
    }
    Ok((out, spec.sample_rate))
}

fn read_flac_i16(path: &str) -> Result<(Vec<i16>, u32), String> {
    let mut reader = claxon::FlacReader::open(path).map_err(|e| e.to_string())?;
    let info = reader.streaminfo();
    let ch = info.channels.max(1) as usize;
    let bits = info.bits_per_sample as i32;
    let mut out = Vec::new();
    for (i, s) in reader.samples().enumerate() {
        let s = s.map_err(|e| e.to_string())?;
        if i % ch == 0 {
            let v = if bits > 16 {
                s >> (bits - 16)
            } else if bits < 16 {
                s << (16 - bits)
            } else {
                s
            };
            out.push(v.clamp(i16::MIN as i32, i16::MAX as i32) as i16);
        }
    }
    Ok((out, info.sample_rate))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn to_dbfs_matches_convention() {
        assert_eq!(to_dbfs(1.0), 0.0);
        assert_eq!(to_dbfs(0.5), -6.0);
        assert_eq!(to_dbfs(0.1), -20.0);
        assert_eq!(to_dbfs(0.0), -60.0);
        assert_eq!(to_dbfs(-1.0), -60.0);
        assert_eq!(to_dbfs(0.0005), -60.0);
        // A level just under full scale rounds toward zero: assert it is
        // positive zero (bit-for-bit), not -0.0, so JSON emits "0" like Go.
        assert_eq!(to_dbfs(0.999).to_bits(), 0.0_f32.to_bits());
    }

    #[test]
    fn wav_round_trips_i16() {
        let dir = std::env::temp_dir();
        let path = dir.join("wavio_roundtrip.wav");
        let samples: Vec<i16> = vec![0, 100, -100, 32767, -32768, 5];
        write_wav_i16(path.to_str().unwrap(), &samples, 48000).unwrap();
        let (back, rate) = read_audio_i16(path.to_str().unwrap()).unwrap();
        assert_eq!(rate, 48000);
        assert_eq!(back, samples);
        let _ = std::fs::remove_file(path);
    }
}
