// Regression test for the Profile B mark/space "twist" (GRA-130 / graywolf
// #324). Profile B (FM discriminator) decodes through a single center-frequency
// oscillator and has no separate mark/space tones. Its per-packet audio level
// used to copy that one center envelope into both the mark and space peaks, so
// every Profile-B-decoded frame reported identical mark and space levels — the
// packet log could never show the mark/space twist that Direwolf reports
// (roughly 2:1). The fix gives Profile B a parallel mark/space oscillator pair
// purely for level metering; the FM discriminator decode path is unchanged.
//
// This decodes a real 1200-baud AFSK recording through a single Profile B
// demodulator and asserts the emitted frames carry distinct, positive mark and
// space levels. Before the fix mark == space on every frame and this fails;
// after the fix they differ and it passes.
use std::fs::File;
use std::io::{BufReader, Read};

use graywolfmodem::demod_afsk::AfskDemodulator;
use graywolfmodem::types::AfskProfile;

fn read_wav_mono16(path: &str) -> (Vec<i16>, u32) {
    let mut r = BufReader::new(File::open(path).unwrap());
    let mut all = Vec::new();
    r.read_to_end(&mut all).unwrap();
    let mut i = 12;
    let mut sr = 44100u32;
    let mut data: Vec<i16> = Vec::new();
    while i + 8 <= all.len() {
        let id = &all[i..i + 4];
        let size = u32::from_le_bytes([all[i + 4], all[i + 5], all[i + 6], all[i + 7]]) as usize;
        let body = i + 8;
        if id == b"fmt " {
            sr = u32::from_le_bytes([all[body + 4], all[body + 5], all[body + 6], all[body + 7]]);
        } else if id == b"data" {
            for c in all[body..(body + size).min(all.len())].chunks_exact(2) {
                data.push(i16::from_le_bytes([c[0], c[1]]));
            }
        }
        i = body + size + (size & 1);
    }
    (data, sr)
}

#[test]
fn profile_b_meters_mark_and_space_independently() {
    let (samples, sr) = read_wav_mono16("testdata/wav/afsk_1200.wav");
    let mut demod = AfskDemodulator::new(sr, 1200, 1200, 2200, AfskProfile::B, 0, 0);
    let mut frames = Vec::new();
    for s in samples {
        demod.process_sample(s as i32);
        frames.extend(demod.take_frames());
    }
    frames.extend(demod.take_frames());

    assert!(!frames.is_empty(), "expected Profile B to decode at least one frame");

    // GRA-84 guarantee: a decoded frame always carries a positive level.
    let level_less = frames
        .iter()
        .filter(|f| f.audio_level_mark <= 0.0 && f.audio_level_space <= 0.0)
        .count();
    assert_eq!(
        level_less, 0,
        "{}/{} Profile B frames carry no audio level",
        level_less,
        frames.len()
    );

    // The twist: at least one frame must report a meaningful gap between the
    // mark and space levels. With the old single-center-envelope metering
    // every frame had mark == space bit-for-bit, so the gap was always 0; a
    // bare `!=` would also pass on incidental float rounding, so require a
    // real separation (5% of the larger peak) to document the intent.
    let max_twist = frames
        .iter()
        .map(|f| {
            let gap = (f.audio_level_mark - f.audio_level_space).abs();
            let scale = f.audio_level_mark.max(f.audio_level_space).max(1e-6);
            gap / scale
        })
        .fold(0.0f32, f32::max);
    assert!(
        max_twist > 0.05,
        "all {} Profile B frames have ~identical mark/space levels (max twist {:.4}) — \
         metering still copies one envelope into both peaks",
        frames.len(),
        max_twist
    );
}
