//! Pure TX test-signal generation: CW (Morse) station ID and steady /
//! alternating test tones.
//!
//! No I/O and no audio device — this turns parameters into i16 PCM samples.
//! The modem submits the samples as a normal TxJob, so PTT keying and
//! play-out reuse the existing TX worker path.

const AMP: f32 = 0.6 * 32767.0;
const RAMP_MS: f32 = 5.0;

/// One keyed or unkeyed CW span, in Morse time units (1 unit = 1 dit).
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct Segment {
    pub on: bool,
    pub units: u32,
}

fn ramp_samples(sample_rate: u32) -> usize {
    ((sample_rate as f32) * RAMP_MS / 1000.0) as usize
}

/// Apply a raised-cosine rise/fall (in place) to suppress edge clicks.
fn apply_edges(buf: &mut [i16], ramp: usize) {
    let n = buf.len();
    let r = ramp.min(n / 2);
    if r == 0 {
        return;
    }
    for i in 0..r {
        let env = 0.5 * (1.0 - (std::f32::consts::PI * i as f32 / r as f32).cos());
        buf[i] = (buf[i] as f32 * env) as i16;
        buf[n - 1 - i] = (buf[n - 1 - i] as f32 * env) as i16;
    }
}

/// Dot/dash pattern for a character, or None when there is no standard Morse
/// representation (encode skips those).
fn pattern(c: char) -> Option<&'static str> {
    Some(match c.to_ascii_uppercase() {
        'A' => ".-", 'B' => "-...", 'C' => "-.-.", 'D' => "-..",
        'E' => ".", 'F' => "..-.", 'G' => "--.", 'H' => "....",
        'I' => "..", 'J' => ".---", 'K' => "-.-", 'L' => ".-..",
        'M' => "--", 'N' => "-.", 'O' => "---", 'P' => ".--.",
        'Q' => "--.-", 'R' => ".-.", 'S' => "...", 'T' => "-",
        'U' => "..-", 'V' => "...-", 'W' => ".--", 'X' => "-..-",
        'Y' => "-.--", 'Z' => "--..",
        '0' => "-----", '1' => ".----", '2' => "..---", '3' => "...--",
        '4' => "....-", '5' => ".....", '6' => "-....", '7' => "--...",
        '8' => "---..", '9' => "----.",
        '/' => "-..-.", '-' => "-....-", '.' => ".-.-.-", ',' => "--..--",
        '?' => "..--..",
        _ => return None,
    })
}

/// Encode text into keyed/unkeyed segments with standard Morse timing:
/// dit=1, dah=3, intra-character gap=1, inter-character gap=3, word gap=7
/// (in dit units). No leading/trailing gaps. Unknown characters are skipped.
pub fn encode(text: &str) -> Vec<Segment> {
    let mut out: Vec<Segment> = Vec::new();
    let mut prev_was_char = false;
    for raw in text.chars() {
        if raw == ' ' {
            if prev_was_char {
                out.push(Segment { on: false, units: 7 });
                prev_was_char = false;
            }
            continue;
        }
        let pat = match pattern(raw) {
            Some(p) => p,
            None => continue,
        };
        if prev_was_char {
            out.push(Segment { on: false, units: 3 });
        }
        for (i, el) in pat.chars().enumerate() {
            if i > 0 {
                out.push(Segment { on: false, units: 1 });
            }
            out.push(Segment { on: true, units: if el == '-' { 3 } else { 1 } });
        }
        prev_was_char = true;
    }
    out
}

/// CW: encode `callsign` and render the on/off-keyed sidetone at `wpm`
/// (PARIS timing) and `tone_hz`, with raised-cosine edges on each keyed span.
/// Returns empty if the callsign has no renderable characters.
pub fn cw_samples(callsign: &str, sample_rate: u32, wpm: u32, tone_hz: f32) -> Vec<i16> {
    let segments = encode(callsign);
    let wpm = wpm.max(1);
    let dit = ((sample_rate as f64) * 1.2 / wpm as f64).round() as usize;
    let ramp = ramp_samples(sample_rate);
    let w = 2.0 * std::f32::consts::PI * tone_hz / sample_rate as f32;
    let mut out: Vec<i16> = Vec::new();
    for seg in &segments {
        let n = dit * seg.units as usize;
        if !seg.on {
            out.extend(std::iter::repeat_n(0i16, n));
            continue;
        }
        let start = out.len();
        for i in 0..n {
            out.push(((w * i as f32).sin() * AMP) as i16);
        }
        let end = out.len();
        apply_edges(&mut out[start..end], ramp);
    }
    out
}

/// Steady sine of `freq_hz` for `duration_ms`, with edge ramps.
pub fn tone_samples(sample_rate: u32, freq_hz: f32, duration_ms: u32) -> Vec<i16> {
    let n = (sample_rate as u64 * duration_ms as u64 / 1000) as usize;
    let w = 2.0 * std::f32::consts::PI * freq_hz / sample_rate as f32;
    let mut out: Vec<i16> = (0..n).map(|i| ((w * i as f32).sin() * AMP) as i16).collect();
    apply_edges(&mut out, ramp_samples(sample_rate));
    out
}

/// Alternating tone: switch between `freq_a` and `freq_b` every `period_ms`
/// for `duration_ms`. Phase-continuous (a running phase accumulator) so the
/// frequency switches don't click; raised-cosine ramps on the outer edges.
pub fn alternating_samples(
    sample_rate: u32,
    freq_a: f32,
    freq_b: f32,
    duration_ms: u32,
    period_ms: u32,
) -> Vec<i16> {
    let n = (sample_rate as u64 * duration_ms as u64 / 1000) as usize;
    let per = ((sample_rate as u64 * period_ms.max(1) as u64 / 1000) as usize).max(1);
    let mut out: Vec<i16> = Vec::with_capacity(n);
    let mut phase = 0.0f32;
    let two_pi = 2.0 * std::f32::consts::PI;
    for i in 0..n {
        let f = if (i / per).is_multiple_of(2) { freq_a } else { freq_b };
        // Wrap to keep f32 phase precise over long durations.
        phase = (phase + two_pi * f / sample_rate as f32) % two_pi;
        out.push((phase.sin() * AMP) as i16);
    }
    apply_edges(&mut out, ramp_samples(sample_rate));
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    fn on(units: u32) -> Segment { Segment { on: true, units } }
    fn off(units: u32) -> Segment { Segment { on: false, units } }

    #[test]
    fn encode_single_dit() {
        assert_eq!(encode("E"), vec![on(1)]);
    }

    #[test]
    fn encode_inter_character_gap() {
        assert_eq!(encode("EE"), vec![on(1), off(3), on(1)]);
    }

    #[test]
    fn encode_dah_and_intra_gaps() {
        // "K" = -.-  => dah, gap, dit, gap, dah
        assert_eq!(encode("K"), vec![on(3), off(1), on(1), off(1), on(3)]);
    }

    #[test]
    fn encode_word_gap() {
        // "A B": A=.- ; word gap 7 ; B=-...
        assert_eq!(
            encode("A B"),
            vec![
                on(1), off(1), on(3),
                off(7),
                on(3), off(1), on(1), off(1), on(1), off(1), on(1),
            ]
        );
    }

    #[test]
    fn encode_skips_unknown_chars() {
        assert_eq!(encode("E@E"), encode("EE"));
    }

    #[test]
    fn cw_samples_dit_length_matches_wpm() {
        // 20 WPM at 48 kHz: dit = 1.2/20 s = 60 ms = 2880 samples.
        let s = cw_samples("E", 48_000, 20, 700.0);
        assert_eq!(s.len(), 2880);
        assert!(s.iter().any(|&v| v != 0), "tone must be non-silent");
    }

    #[test]
    fn cw_samples_empty_callsign_is_empty() {
        assert!(cw_samples("", 48_000, 20, 700.0).is_empty());
    }

    #[test]
    fn tone_samples_length_and_nonsilent() {
        // 3000 ms at 48 kHz = 144000 samples.
        let s = tone_samples(48_000, 1200.0, 3000);
        assert_eq!(s.len(), 144_000);
        assert!(s.iter().any(|&v| v != 0));
    }

    #[test]
    fn alternating_samples_length_and_nonsilent() {
        let s = alternating_samples(48_000, 1200.0, 2400.0, 3000, 200);
        assert_eq!(s.len(), 144_000);
        assert!(s.iter().any(|&v| v != 0));
    }

    #[test]
    fn alternating_samples_actually_alternates_frequency() {
        // 200 ms period at 48 kHz = 9600 samples/block. Block 0 renders
        // freq_a (1200 Hz), block 1 renders freq_b (2400 Hz). The higher
        // tone must show clearly more zero crossings than the lower one.
        let s = alternating_samples(48_000, 1200.0, 2400.0, 3000, 200);
        let per = 9600usize;
        let zero_crossings =
            |w: &[i16]| w.windows(2).filter(|p| (p[0] >= 0) != (p[1] >= 0)).count();
        let lo = zero_crossings(&s[0..per]);
        let hi = zero_crossings(&s[per..2 * per]);
        assert!(
            hi > lo * 3 / 2,
            "freq_b block ({hi} crossings) should clearly exceed freq_a block ({lo})"
        );
    }
}
