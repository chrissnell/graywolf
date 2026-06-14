// Regression test for GRA-84: every frame the default RECOMMENDED_3DEMOD
// ensemble emits must carry a valid (positive) per-packet audio level.
//
// The bug: Profile B (FM discriminator) never tracked an audio level, leaving
// its mark/space peaks at the -1.0 init. Profile B usually wins the cross-demod
// dedup, so the emitted frame reported no level and the packet log rendered a
// dash for nearly every packet. This decodes a real 1200-baud AFSK recording
// through the production ensemble and asserts no frame comes out level-less.
use std::fs::File;
use std::io::{BufReader, Read};

use graywolfmodem::demod_afsk_multi::{MultiAfskDemodulator, RECOMMENDED_3DEMOD};

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
fn ensemble_frames_carry_audio_level() {
    let (samples, sr) = read_wav_mono16("testdata/wav/afsk_1200.wav");
    let mut demod = MultiAfskDemodulator::new(sr, 1200, 1200, 2200, 0, &RECOMMENDED_3DEMOD);
    let mut frames = Vec::new();
    for s in samples {
        demod.process_sample(s as i32);
        frames.extend(demod.take_frames());
    }
    frames.extend(demod.take_frames());

    assert!(!frames.is_empty(), "expected to decode at least one frame");
    let dashed = frames
        .iter()
        .filter(|f| f.audio_level_mark <= 0.0 && f.audio_level_space <= 0.0)
        .count();
    assert_eq!(
        dashed, 0,
        "{}/{} decoded frames carry no audio level (would render as a dash in the packet log)",
        dashed,
        frames.len()
    );
}
