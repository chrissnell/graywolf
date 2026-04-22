//! Multi-demodulator harness.
//!
//! Runs a set of named demodulator configurations against the same audio file
//! and reports per-configuration counts plus various cross-configuration
//! union statistics. Useful for evaluating which combinations of demod
//! profiles / slicer counts / hard-limiter settings cover the largest set of
//! recoverable frames on each track.
//!
//! Metrics reported per configuration:
//!   raw     — every frame event emitted by the HDLC decoders, including
//!             within-multi-slicer duplicates (comparable to Direwolf's
//!             "packets decoded" only when the decoder runs at 1 slicer).
//!   unique  — distinct frame-content values seen (a repeating station
//!             beacon counts as 1 even if emitted 10 times over the file).
//!   events  — frame events after per-config timestamp dedup: the same
//!             content at sample offsets within SAMPLE_WINDOW counts once.
//!             This is the apples-to-apples analogue of Direwolf's
//!             multi-slicer-deduped "packets decoded" number.
//!
//! Cross-configuration union metrics use the events definition, taking the
//! set union across all listed configurations with the same timestamp
//! window.

use std::collections::HashSet;
use std::env;
use std::fs::File;
use std::io::{self, BufReader, Read, Seek, SeekFrom};
use std::time::Instant;

use graywolf_demod::demod_afsk::AfskDemodulator;
use graywolf_demod::demod_afsk_multi::{MultiAfskDemodulator, MultiConfig};
use graywolf_demod::hdlc::DecodedFrame;
use graywolf_demod::types::*;

/// Sample window used to merge identical-content frames into a single event.
/// Matches Direwolf's `multi_modem.c` PROCESS_AFTER_BITS = 3 — three symbol
/// times. At 1200 baud / 44100 sps that's 3 * 44100 / 1200 ≈ 110 samples.
/// Narrow enough that legitimate fast rebroadcasts (digipeaters, APRS-IS
/// injection) count as separate events, wide enough that a single packet
/// decoded by multiple slicers / profiles within one symbol collapses to one.
const SAMPLE_WINDOW: u64 = 110;

#[derive(Clone)]
struct Cfg {
    name: &'static str,
    profile: AfskProfile,
    slicers: usize,
    hard_limit: bool,
}

fn read_flac(path: &str) -> io::Result<(Vec<i16>, u32, u32)> {
    let mut reader = claxon::FlacReader::open(path)
        .map_err(|e| io::Error::new(io::ErrorKind::InvalidData, e.to_string()))?;
    let info = reader.streaminfo();
    let sr = info.sample_rate;
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
    Ok((samples, sr, channels))
}

fn read_wav(path: &str) -> io::Result<(Vec<i16>, u32, u32)> {
    let file = File::open(path)?;
    let mut r = BufReader::new(file);
    let mut b4 = [0u8; 4];
    let mut b2 = [0u8; 2];
    r.read_exact(&mut b4)?;
    if &b4 != b"RIFF" {
        return Err(io::Error::new(io::ErrorKind::InvalidData, "not RIFF"));
    }
    r.read_exact(&mut b4)?;
    r.read_exact(&mut b4)?;
    if &b4 != b"WAVE" {
        return Err(io::Error::new(io::ErrorKind::InvalidData, "not WAVE"));
    }
    let mut sr = 0u32;
    let mut ch = 0u16;
    let mut bps = 0u16;
    let mut data: Vec<i16> = Vec::new();
    loop {
        if r.read_exact(&mut b4).is_err() {
            break;
        }
        let id = b4;
        r.read_exact(&mut b4)?;
        let size = u32::from_le_bytes(b4);
        if &id == b"fmt " {
            r.read_exact(&mut b2)?;
            let _ = u16::from_le_bytes(b2);
            r.read_exact(&mut b2)?;
            ch = u16::from_le_bytes(b2);
            r.read_exact(&mut b4)?;
            sr = u32::from_le_bytes(b4);
            r.read_exact(&mut b4)?;
            r.read_exact(&mut b2)?;
            r.read_exact(&mut b2)?;
            bps = u16::from_le_bytes(b2);
            if size > 16 {
                r.seek(SeekFrom::Current((size - 16) as i64))?;
            }
        } else if &id == b"data" {
            if bps == 16 {
                let mut buf = vec![0u8; size as usize];
                r.read_exact(&mut buf)?;
                for c in buf.chunks_exact(2) {
                    data.push(i16::from_le_bytes([c[0], c[1]]));
                }
            } else {
                return Err(io::Error::new(
                    io::ErrorKind::InvalidData,
                    "unsupported bits",
                ));
            }
        } else {
            let skip = (size + 1) & !1;
            r.seek(SeekFrom::Current(skip as i64))?;
        }
    }
    Ok((data, sr, ch as u32))
}

/// Per-config event dedup: a (content, offset) pair collapses all frames
/// with the same content whose offsets are within SAMPLE_WINDOW of each
/// other into the earliest-offset representative.
fn dedupe_to_events(mut frames: Vec<DecodedFrame>) -> Vec<(Vec<u8>, u64)> {
    frames.sort_by_key(|f| f.sample_offset);
    // (content, offset) with a guard map keyed by content tracking last offset.
    let mut out: Vec<(Vec<u8>, u64)> = Vec::new();
    let mut last_offset: std::collections::HashMap<Vec<u8>, u64> =
        std::collections::HashMap::new();
    for f in frames {
        let prev = last_offset.get(&f.data).copied();
        let keep = match prev {
            Some(p) => f.sample_offset.saturating_sub(p) >= SAMPLE_WINDOW,
            None => true,
        };
        if keep {
            out.push((f.data.clone(), f.sample_offset));
            last_offset.insert(f.data, f.sample_offset);
        }
    }
    out
}

/// Live cross-demod dedup through the MultiAfskDemodulator library primitive.
/// Produces the leaderboard "events" number directly, as an integration test of
/// the library API vs. the bench-mode offline dedup above.
fn run_library_mode(cfgs: &[Cfg], samples: &[i16], sample_rate: u32, dur: f64) {
    let multi_cfgs: Vec<MultiConfig> = cfgs
        .iter()
        .map(|c| MultiConfig {
            profile: c.profile,
            slicers: c.slicers,
            hard_limit: c.hard_limit,
        })
        .collect();
    let mut demod = MultiAfskDemodulator::new(
        sample_rate,
        DEFAULT_BAUD,
        DEFAULT_MARK_FREQ,
        DEFAULT_SPACE_FREQ,
        0,
        &multi_cfgs,
    );
    let start = Instant::now();
    for &s in samples {
        demod.process_sample(s as i32);
    }
    let elapsed = start.elapsed().as_secs_f64();
    let frames = demod.take_frames();
    let unique: HashSet<Vec<u8>> = frames.iter().map(|f| f.data.clone()).collect();
    eprintln!();
    eprintln!("=== MultiAfskDemodulator (live dedup) ===");
    eprintln!("  events           : {}", frames.len());
    eprintln!("  unique contents  : {}", unique.len());
    eprintln!(
        "  wall time        : {:.2}s  ({:.0}x realtime)",
        elapsed,
        dur / elapsed
    );
}

fn run_cfg(cfg: &Cfg, samples: &[i16], sample_rate: u32) -> Vec<DecodedFrame> {
    let mut demod = AfskDemodulator::new(
        sample_rate,
        DEFAULT_BAUD,
        DEFAULT_MARK_FREQ,
        DEFAULT_SPACE_FREQ,
        cfg.profile,
        0,
        0,
    );
    if cfg.slicers > 1 {
        demod.set_num_slicers(cfg.slicers);
    }
    if cfg.hard_limit {
        demod.set_hard_limit(true);
    }
    for &s in samples {
        demod.process_sample(s as i32);
    }
    demod.take_frames()
}

fn main() {
    let args: Vec<String> = env::args().collect();
    if args.len() < 2 || args.iter().any(|a| a == "-h" || a == "--help") {
        eprintln!("Usage: demod-multi <audio-file> [cfg-list] [--library]");
        eprintln!();
        eprintln!("cfg-list:  comma-separated names drawn from");
        eprintln!("           A1 A6 A9 A1HL A6HL A9HL B1 B9 B1HL B9HL");
        eprintln!("           where the letter is the profile, the number is the slicer");
        eprintln!("           count (1/6/9), and a trailing HL enables the sign(x) hard");
        eprintln!("           limiter before the bandpass prefilter. Default: all 10.");
        eprintln!();
        eprintln!("Notable presets:");
        eprintln!("  recommended 2-demod:  A9,A9HL           (~1.1% of a core)");
        eprintln!("  recommended 3-demod:  A9,A9HL,B9        (~1.6% of a core)");
        eprintln!();
        eprintln!("--library   run the set through MultiAfskDemodulator with live");
        eprintln!("            cross-demod dedup (integration-test mode). Default is");
        eprintln!("            bench mode: run each config separately, dedup at end.");
        std::process::exit(if args.len() < 2 { 1 } else { 0 });
    }
    let file = &args[1];
    let use_library = args.iter().any(|a| a == "--library");

    // Default candidate set spans single-slicer, deep-multi-slicer, and HL.
    let all: Vec<Cfg> = vec![
        Cfg { name: "A1",    profile: AfskProfile::A, slicers: 1, hard_limit: false },
        Cfg { name: "A6",    profile: AfskProfile::A, slicers: 6, hard_limit: false },
        Cfg { name: "A9",    profile: AfskProfile::A, slicers: 9, hard_limit: false },
        Cfg { name: "A1HL",  profile: AfskProfile::A, slicers: 1, hard_limit: true  },
        Cfg { name: "A6HL",  profile: AfskProfile::A, slicers: 6, hard_limit: true  },
        Cfg { name: "A9HL",  profile: AfskProfile::A, slicers: 9, hard_limit: true  },
        Cfg { name: "B1",    profile: AfskProfile::B, slicers: 1, hard_limit: false },
        Cfg { name: "B9",    profile: AfskProfile::B, slicers: 9, hard_limit: false },
        Cfg { name: "B1HL",  profile: AfskProfile::B, slicers: 1, hard_limit: true  },
        Cfg { name: "B9HL",  profile: AfskProfile::B, slicers: 9, hard_limit: true  },
    ];

    // First non-flag arg after the filename is the cfg-list.
    let cfg_arg = args.iter().skip(2).find(|a| !a.starts_with("--"));
    let cfgs: Vec<Cfg> = if let Some(spec) = cfg_arg {
        let wanted: HashSet<&str> = spec.split(',').collect();
        let chosen: Vec<Cfg> = all
            .iter()
            .filter(|c| wanted.contains(c.name))
            .cloned()
            .collect();
        if chosen.is_empty() {
            eprintln!("error: no configs matched {:?}", spec);
            std::process::exit(1);
        }
        chosen
    } else {
        all
    };

    let (samples, sample_rate, channels) =
        if file.ends_with(".wav") {
            read_wav(file).expect("read wav")
        } else {
            read_flac(file).expect("read flac")
        };

    let samples: Vec<i16> = if channels > 1 {
        samples.chunks(channels as usize).map(|c| c[0]).collect()
    } else {
        samples
    };

    let dur = samples.len() as f64 / sample_rate as f64;
    eprintln!(
        "{}: {:.1} s audio at {} sps, {} configs ({} mode)",
        file,
        dur,
        sample_rate,
        cfgs.len(),
        if use_library { "library" } else { "bench" }
    );

    if use_library {
        run_library_mode(&cfgs, &samples, sample_rate, dur);
        return;
    }

    struct CfgRun {
        cfg: Cfg,
        events: Vec<(Vec<u8>, u64)>,
    }

    let start = Instant::now();
    let mut per_cfg: Vec<CfgRun> = Vec::new();
    for cfg in &cfgs {
        let t0 = Instant::now();
        let frames = run_cfg(cfg, &samples, sample_rate);
        let raw = frames.len();
        let uniq: HashSet<Vec<u8>> = frames.iter().map(|f| f.data.clone()).collect();
        let unique_count = uniq.len();
        let events = dedupe_to_events(frames);
        let elapsed = t0.elapsed().as_secs_f64();
        eprintln!(
            "  {:6} raw={:5} unique={:4} events={:4}  ({:.2}s, {:.0}x realtime)",
            cfg.name, raw, unique_count, events.len(), elapsed, dur / elapsed
        );
        let _ = raw;
        let _ = unique_count;
        per_cfg.push(CfgRun { cfg: cfg.clone(), events });
    }
    let total_elapsed = start.elapsed().as_secs_f64();

    // Event-window dedup across configs: same content merged within window.
    // Per-config events are already within-config window-deduped; flatten and
    // sort by (content, offset) so we can collapse cross-config near-duplicates
    // in one pass.
    let mut flat: Vec<(Vec<u8>, u64)> = Vec::new();
    for run in &per_cfg {
        flat.extend(run.events.iter().cloned());
    }
    flat.sort_by(|a, b| a.0.cmp(&b.0).then(a.1.cmp(&b.1)));
    let mut union_event_count = 0;
    let mut last_content: Option<&[u8]> = None;
    let mut last_off: u64 = 0;
    for (content, off) in &flat {
        let same = last_content == Some(content.as_slice())
            && off.saturating_sub(last_off) < SAMPLE_WINDOW;
        if !same {
            union_event_count += 1;
            last_content = Some(content.as_slice());
            last_off = *off;
        }
    }

    // Content-only union (identical to unique-content set union).
    let union_unique: HashSet<&Vec<u8>> = per_cfg
        .iter()
        .flat_map(|run| run.events.iter().map(|(c, _)| c))
        .collect();

    eprintln!();
    eprintln!("=== Union across all {} configs ===", cfgs.len());
    eprintln!(
        "  union unique contents : {}",
        union_unique.len()
    );
    eprintln!(
        "  union events (window) : {}",
        union_event_count
    );
    eprintln!(
        "  total wall time       : {:.2}s  ({:.0}x realtime)",
        total_elapsed,
        dur / total_elapsed
    );

    // Also compute coverage-missed-by-X sort (sorted by how many unique
    // contents each single config contributes that no other config caught).
    eprintln!();
    eprintln!("=== Per-config unique-contribution (vs. other configs) ===");
    let all_sets: Vec<HashSet<Vec<u8>>> = per_cfg
        .iter()
        .map(|run| run.events.iter().map(|(c, _)| c.clone()).collect())
        .collect();
    for (i, run) in per_cfg.iter().enumerate() {
        let others: HashSet<Vec<u8>> = all_sets
            .iter()
            .enumerate()
            .filter(|(j, _)| *j != i)
            .flat_map(|(_, s)| s.iter().cloned())
            .collect();
        let unique_to_me = all_sets[i].difference(&others).count();
        eprintln!("  {:6} contributes {:3} uniquely", run.cfg.name, unique_to_me);
    }
}
