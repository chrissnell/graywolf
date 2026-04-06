use criterion::{black_box, criterion_group, criterion_main, Criterion};
use graywolf_demod::demod_afsk::AfskDemodulator;
use graywolf_demod::types::*;

fn bench_process_sample(c: &mut Criterion) {
    let mut demod = AfskDemodulator::new(
        DEFAULT_SAMPLES_PER_SEC,
        DEFAULT_BAUD,
        DEFAULT_MARK_FREQ,
        DEFAULT_SPACE_FREQ,
        AfskProfile::A,
        0,
        0,
    );

    let sample_count = 44100;
    let samples: Vec<i16> = (0..sample_count)
        .map(|i| {
            let t = i as f32 / DEFAULT_SAMPLES_PER_SEC as f32;
            (16000.0 * (2.0 * std::f32::consts::PI * 1200.0 * t).sin()) as i16
        })
        .collect();

    c.bench_function("process_sample_1s_profile_a", |b| {
        b.iter(|| {
            for &s in &samples {
                demod.process_sample(black_box(s as i32));
            }
        })
    });
}

fn bench_process_sample_profile_b(c: &mut Criterion) {
    let mut demod = AfskDemodulator::new(
        DEFAULT_SAMPLES_PER_SEC,
        DEFAULT_BAUD,
        DEFAULT_MARK_FREQ,
        DEFAULT_SPACE_FREQ,
        AfskProfile::B,
        0,
        0,
    );

    let sample_count = 44100;
    let samples: Vec<i16> = (0..sample_count)
        .map(|i| {
            let t = i as f32 / DEFAULT_SAMPLES_PER_SEC as f32;
            (16000.0 * (2.0 * std::f32::consts::PI * 1200.0 * t).sin()) as i16
        })
        .collect();

    c.bench_function("process_sample_1s_profile_b", |b| {
        b.iter(|| {
            for &s in &samples {
                demod.process_sample(black_box(s as i32));
            }
        })
    });
}

criterion_group!(benches, bench_process_sample, bench_process_sample_profile_b);
criterion_main!(benches);
