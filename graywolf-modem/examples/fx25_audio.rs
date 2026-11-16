//! Offline interoperability harness. Never opens audio or PTT devices.
use graywolfmodem::{
    demod_afsk_multi::{MultiAfskDemodulator, RECOMMENDED_3DEMOD},
    fx25, tx,
    types::RetryType,
};
fn unhex(s: &str) -> Vec<u8> {
    (0..s.len())
        .step_by(2)
        .map(|i| u8::from_str_radix(&s[i..i + 2], 16).unwrap())
        .collect()
}
fn hex(b: &[u8]) -> String {
    b.iter().map(|b| format!("{b:02x}")).collect()
}
fn main() {
    let args: Vec<String> = std::env::args().collect();
    match args[1].as_str() {
        "encode" | "encode-mode" => {
            let frame = unhex(&args[2]);
            let tag = if args[1] == "encode" {
                args.get(4).map(|s| s.parse().unwrap())
            } else {
                None
            };
            let samples = if args[1] == "encode-mode" {
                let parity = if args[4] == "auto" {
                    0
                } else {
                    args[4].parse().unwrap()
                };
                tx::build_fx25_samples_with_parity(
                    &frame, 300, 100, 48000, 1200, 1200, 2200, parity,
                )
                .unwrap()
            } else if let Some(tag) = tag {
                let wire = fx25::encode(&frame, Some(tag)).expect("frame does not fit");
                let mut line = 0;
                let bits: Vec<u8> = std::iter::repeat_n(0x7e, 45)
                    .chain(wire)
                    .chain(std::iter::repeat_n(0x7e, 15))
                    .flat_map(|b| (0..8).map(move |s| (b >> s) & 1))
                    .map(|b| {
                        if b == 0 {
                            line ^= 1;
                        }
                        line
                    })
                    .collect();
                tx::afsk_mod::modulate(&bits, 48000, 1200, 1200, 2200).unwrap()
            } else {
                tx::build_fx25_samples(&frame, 300, 100, 48000, 1200, 1200, 2200).unwrap()
            };
            let spec = hound::WavSpec {
                channels: 1,
                sample_rate: 48000,
                bits_per_sample: 16,
                sample_format: hound::SampleFormat::Int,
            };
            let mut writer = hound::WavWriter::create(&args[3], spec).unwrap();
            for s in samples {
                writer.write_sample(s).unwrap();
            }
            writer.finalize().unwrap();
        }
        "decode" => {
            let mut wav = hound::WavReader::open(&args[2]).unwrap();
            let spec = wav.spec();
            assert_eq!(spec.channels, 1);
            let mut demod = MultiAfskDemodulator::new(
                spec.sample_rate,
                1200,
                1200,
                2200,
                0,
                &RECOMMENDED_3DEMOD,
            );
            for sample in wav.samples::<i16>() {
                demod.process_sample(sample.unwrap() as i32);
                for f in demod.take_frames() {
                    println!(
                        "{} {}",
                        hex(&f.data),
                        if f.retry == RetryType::Fx25 {
                            "fx25"
                        } else {
                            "ax25"
                        }
                    );
                }
            }
        }
        "workload" => {
            let repeats: usize = args[3].parse().unwrap();
            let spec = hound::WavReader::open(&args[2]).unwrap().spec();
            assert_eq!(spec.channels, 1);
            let mut demod = MultiAfskDemodulator::new(
                spec.sample_rate,
                1200,
                1200,
                2200,
                0,
                &RECOMMENDED_3DEMOD,
            );
            let start = std::time::Instant::now();
            let (mut count, mut samples) = (0usize, 0usize);
            for _ in 0..repeats {
                let mut wav = hound::WavReader::open(&args[2]).unwrap();
                for sample in wav.samples::<i16>() {
                    demod.process_sample(sample.unwrap() as i32);
                    count += demod.take_frames().len();
                    samples += 1;
                }
            }
            println!(
                "frames={count} samples={samples} audio_seconds={:.3} elapsed_seconds={:.3}",
                samples as f64 / spec.sample_rate as f64,
                start.elapsed().as_secs_f64()
            );
        }
        "wire" => println!(
            "{}",
            hex(&fx25::encode(&unhex(&args[2]), Some(args[3].parse().unwrap())).unwrap())
        ),
        _ => panic!("encode HEX WAV [TAG] | decode WAV | wire HEX TAG | workload WAV REPEATS"),
    }
}
