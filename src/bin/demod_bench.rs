use std::env;
use std::fs::File;
use std::io::{self, BufReader, Read, Seek, SeekFrom};
use std::time::Instant;

use graywolf_demod::demod_afsk::AfskDemodulator;
use graywolf_demod::types::*;

fn usage() {
    eprintln!("Usage: demod-bench [options] <audio-file>");
    eprintln!();
    eprintln!("Options:");
    eprintln!("  -B n           Bit rate (default 1200)");
    eprintln!("  -P c           Profile: A or B (default A)");
    eprintln!("  -D n           Decimate incoming audio by factor n (1, 2, or 3)");
    eprintln!("  -F n           Fix bits level: 0=none, 1=single, 2=double (default 0)");
    eprintln!("  -S n           Number of slicers (1 to 9, default 1)");
    eprintln!("  --mark n       Mark frequency in Hz (default 1200)");
    eprintln!("  --space n      Space frequency in Hz (default 2200)");
    eprintln!("  --raw-rate n   Sample rate for raw PCM files");
    eprintln!("  --dump-frames  Print decoded frame hex to stdout");
    eprintln!("  --wav          Force WAV parsing");
    eprintln!("  -h             Print this help message");
    std::process::exit(1);
}

struct AudioData {
    samples: Vec<i16>,
    sample_rate: u32,
    channels: u32,
}

fn read_flac(path: &str) -> io::Result<AudioData> {
    let mut reader = claxon::FlacReader::open(path)
        .map_err(|e| io::Error::new(io::ErrorKind::InvalidData, e.to_string()))?;
    let info = reader.streaminfo();
    let sample_rate = info.sample_rate;
    let bits = info.bits_per_sample;
    let channels = info.channels;

    let samples: Vec<i16> = reader
        .samples()
        .map(|s| {
            let s = s.unwrap();
            if bits > 16 {
                (s >> (bits - 16)) as i16
            } else if bits < 16 {
                (s << (16 - bits)) as i16
            } else {
                s as i16
            }
        })
        .collect();

    Ok(AudioData {
        samples,
        sample_rate,
        channels,
    })
}

fn read_wav(path: &str) -> io::Result<AudioData> {
    let file = File::open(path)?;
    let mut reader = BufReader::new(file);

    let mut buf4 = [0u8; 4];
    let mut buf2 = [0u8; 2];

    reader.read_exact(&mut buf4)?;
    if &buf4 != b"RIFF" {
        return Err(io::Error::new(io::ErrorKind::InvalidData, "not a RIFF file"));
    }
    reader.read_exact(&mut buf4)?;
    reader.read_exact(&mut buf4)?;
    if &buf4 != b"WAVE" {
        return Err(io::Error::new(
            io::ErrorKind::InvalidData,
            "not a WAVE file",
        ));
    }

    let mut sample_rate = 0u32;
    let mut channels = 0u16;
    let mut bits_per_sample = 0u16;
    let mut data_samples: Vec<i16> = Vec::new();

    loop {
        if reader.read_exact(&mut buf4).is_err() {
            break;
        }
        let chunk_id = buf4;
        reader.read_exact(&mut buf4)?;
        let chunk_size = u32::from_le_bytes(buf4);

        if &chunk_id == b"fmt " {
            reader.read_exact(&mut buf2)?;
            let format_tag = u16::from_le_bytes(buf2);
            if format_tag != 1 {
                return Err(io::Error::new(
                    io::ErrorKind::InvalidData,
                    format!("unsupported WAV format tag {}", format_tag),
                ));
            }
            reader.read_exact(&mut buf2)?;
            channels = u16::from_le_bytes(buf2);
            reader.read_exact(&mut buf4)?;
            sample_rate = u32::from_le_bytes(buf4);
            reader.read_exact(&mut buf4)?; // byte rate
            reader.read_exact(&mut buf2)?; // block align
            reader.read_exact(&mut buf2)?;
            bits_per_sample = u16::from_le_bytes(buf2);

            let consumed = 16u32;
            if chunk_size > consumed {
                reader.seek(SeekFrom::Current((chunk_size - consumed) as i64))?;
            }
        } else if &chunk_id == b"data" {
            let num_samples = chunk_size as usize / (bits_per_sample as usize / 8);
            data_samples.reserve(num_samples);

            if bits_per_sample == 16 {
                let mut sample_buf = vec![0u8; chunk_size as usize];
                reader.read_exact(&mut sample_buf)?;
                for chunk in sample_buf.chunks_exact(2) {
                    let s = i16::from_le_bytes([chunk[0], chunk[1]]);
                    data_samples.push(s);
                }
            } else if bits_per_sample == 8 {
                let mut sample_buf = vec![0u8; chunk_size as usize];
                reader.read_exact(&mut sample_buf)?;
                for &b in &sample_buf {
                    data_samples.push((b as i16 - 128) * 256);
                }
            } else {
                return Err(io::Error::new(
                    io::ErrorKind::InvalidData,
                    format!("unsupported bits per sample: {}", bits_per_sample),
                ));
            }
        } else {
            let skip = (chunk_size + 1) & !1;
            reader.seek(SeekFrom::Current(skip as i64))?;
        }
    }

    Ok(AudioData {
        samples: data_samples,
        sample_rate,
        channels: channels as u32,
    })
}

fn read_raw_pcm(path: &str, sample_rate: u32) -> io::Result<AudioData> {
    let mut file = File::open(path)?;
    let mut data = Vec::new();
    file.read_to_end(&mut data)?;

    let samples: Vec<i16> = data
        .chunks_exact(2)
        .map(|chunk| i16::from_le_bytes([chunk[0], chunk[1]]))
        .collect();

    Ok(AudioData {
        samples,
        sample_rate,
        channels: 1,
    })
}

fn main() {
    let args: Vec<String> = env::args().collect();

    let mut baud = DEFAULT_BAUD;
    let mut profile = AfskProfile::A;
    let mut mark_freq = DEFAULT_MARK_FREQ;
    let mut space_freq = DEFAULT_SPACE_FREQ;
    let mut raw_rate: Option<u32> = None;
    let mut dump_frames = false;
    let mut force_wav = false;
    let mut decimate: u32 = 1;
    let mut fix_bits = RetryType::None;
    let mut num_slicers: usize = 1;
    let mut file_path: Option<String> = None;

    let mut i = 1;
    while i < args.len() {
        match args[i].as_str() {
            "-B" => {
                i += 1;
                baud = args[i].parse().expect("invalid baud rate");
            }
            "-P" => {
                i += 1;
                profile = match args[i].as_str() {
                    "A" | "a" => AfskProfile::A,
                    "B" | "b" => AfskProfile::B,
                    _ => {
                        eprintln!("Invalid profile: {}", args[i]);
                        std::process::exit(1);
                    }
                };
            }
            "-D" => {
                i += 1;
                decimate = args[i].parse().expect("invalid decimation factor");
                if !(1..=3).contains(&decimate) {
                    eprintln!("Decimation factor must be 1, 2, or 3");
                    std::process::exit(1);
                }
            }
            "-F" => {
                i += 1;
                fix_bits = match args[i].as_str() {
                    "0" => RetryType::None,
                    "1" => RetryType::InvertSingle,
                    "2" => RetryType::InvertDouble,
                    _ => {
                        eprintln!("Fix bits level must be 0, 1, or 2");
                        std::process::exit(1);
                    }
                };
            }
            "-S" => {
                i += 1;
                num_slicers = args[i].parse().expect("invalid slicer count");
                if !(1..=MAX_SLICERS).contains(&num_slicers) {
                    eprintln!("Slicer count must be 1 to {}", MAX_SLICERS);
                    std::process::exit(1);
                }
            }
            "--mark" => {
                i += 1;
                mark_freq = args[i].parse().expect("invalid mark frequency");
            }
            "--space" => {
                i += 1;
                space_freq = args[i].parse().expect("invalid space frequency");
            }
            "--raw-rate" => {
                i += 1;
                raw_rate = Some(args[i].parse().expect("invalid sample rate"));
            }
            "--dump-frames" => {
                dump_frames = true;
            }
            "--wav" => {
                force_wav = true;
            }
            "-h" | "--help" => {
                usage();
            }
            arg if arg.starts_with('-') => {
                eprintln!("Unknown option: {}", arg);
                usage();
            }
            _ => {
                file_path = Some(args[i].clone());
            }
        }
        i += 1;
    }

    let file_path = match file_path {
        Some(p) => p,
        None => {
            eprintln!("No input file specified.");
            usage();
            unreachable!()
        }
    };

    // Load audio
    let audio = if let Some(rate) = raw_rate {
        read_raw_pcm(&file_path, rate).expect("failed to read raw PCM")
    } else if force_wav || file_path.ends_with(".wav") {
        read_wav(&file_path).expect("failed to read WAV")
    } else if file_path.ends_with(".flac") {
        read_flac(&file_path).expect("failed to read FLAC")
    } else {
        read_flac(&file_path)
            .or_else(|_| read_wav(&file_path))
            .expect("failed to read audio file (tried FLAC and WAV)")
    };

    let mut sample_rate = audio.sample_rate;
    let channels = audio.channels;

    // For multi-channel, take only channel 0 (left)
    let mut samples: Vec<i16> = if channels > 1 {
        audio
            .samples
            .chunks(channels as usize)
            .map(|chunk| chunk[0])
            .collect()
    } else {
        audio.samples
    };

    // Apply decimation
    if decimate > 1 {
        samples = samples
            .into_iter()
            .step_by(decimate as usize)
            .collect();
        sample_rate /= decimate;
    }

    eprintln!(
        "{} samples per second. {} total samples. Duration = {:.1} seconds.",
        sample_rate,
        samples.len(),
        samples.len() as f64 / sample_rate as f64
    );

    // Initialize demodulator
    let mut demod = AfskDemodulator::new(sample_rate, baud, mark_freq, space_freq, profile, 0, 0);

    if num_slicers > 1 {
        demod.set_num_slicers(num_slicers);
    }
    if fix_bits != RetryType::None {
        demod.set_fix_bits(fix_bits);
    }

    // Time only the demodulation loop
    let start = Instant::now();
    for &sample in &samples {
        demod.process_sample(sample as i32);
    }
    let elapsed = start.elapsed();

    let packet_count = demod.frame_count();
    let duration_secs = samples.len() as f64 / sample_rate as f64;
    let elapsed_secs = elapsed.as_secs_f64();
    let realtime_ratio = duration_secs / elapsed_secs;

    if dump_frames {
        for frame in demod.take_frames() {
            let hex: Vec<String> = frame.data.iter().map(|b| format!("{:02x}", b)).collect();
            println!("{}", hex.join(" "));
        }
    }

    eprintln!(
        "{} packets decoded in {:.3}s.  {:.1} x realtime",
        packet_count, elapsed_secs, realtime_ratio
    );
}
