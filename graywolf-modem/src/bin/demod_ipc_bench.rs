//! End-to-end benchmark: spawn the real `graywolf-modem` binary, drive it
//! via IPC the way the Go app does, consume every `ReceivedFrame` that
//! comes back, and print a Direwolf-comparable packet count.
//!
//! Unlike `demod-bench` and `demod-multi`, which exercise the demod
//! library directly, this harness exercises the full production path:
//! IPC framing, ConfigureAudio, ConfigureChannel, StartAudio, the
//! audio-pump thread, the frame router, and the ReceivedFrame IPC
//! payloads. If this prints the expected number, the production binary
//! is actually decoding at that level — not just the library.
//!
//! Usage:
//!   demod-ipc-bench <audio-file.flac> [ensemble]
//!
//! `ensemble` is one of "" (= default = triple), "single", "dual",
//! "triple". The default matches the new production default.

use std::io::{BufReader, Read};
use std::path::PathBuf;
use std::process::{Command, Stdio};
use std::time::{Duration, Instant};

#[cfg(unix)]
use std::os::unix::net::UnixStream;
#[cfg(windows)]
use std::net::TcpStream;

use graywolf_demod::ipc::framing::{read_frame, write_frame};
use graywolf_demod::ipc::proto::{
    ipc_message::Payload, ConfigureAudio, ConfigureChannel, IpcMessage, StartAudio,
};

fn graywolf_modem_path() -> PathBuf {
    // Same target-directory discovery as the existing e2e test harness.
    let manifest_dir = PathBuf::from(env!("CARGO_MANIFEST_DIR"));
    let workspace_root = manifest_dir.parent().expect("manifest has a parent");
    let mut p = workspace_root.to_path_buf();
    p.push("target");
    p.push(if cfg!(debug_assertions) { "debug" } else { "release" });
    p.push("graywolf-modem");
    p
}

fn main() {
    let args: Vec<String> = std::env::args().collect();
    if args.len() < 2 {
        eprintln!("Usage: demod-ipc-bench <audio-file> [ensemble]");
        eprintln!("  ensemble: \"\" (default) | \"single\" | \"dual\" | \"triple\"");
        std::process::exit(2);
    }
    let track = PathBuf::from(&args[1]);
    if !track.exists() {
        eprintln!("audio file not found: {}", track.display());
        std::process::exit(1);
    }
    let ensemble = args.get(2).cloned().unwrap_or_default();

    let bin = graywolf_modem_path();
    if !bin.exists() {
        eprintln!("graywolf-modem not built at {}", bin.display());
        eprintln!("run: cargo build --release --bin graywolf-modem");
        std::process::exit(1);
    }

    // Spawn the modem binary.
    #[cfg(unix)]
    let sock = std::env::temp_dir().join(format!(
        "graywolf-ipc-bench-{}-{}.sock",
        std::process::id(),
        Instant::now().elapsed().as_nanos()
    ));
    #[cfg(unix)]
    let _ = std::fs::remove_file(&sock);

    #[cfg(unix)]
    let mut child = Command::new(&bin)
        .arg(&sock)
        .stdout(Stdio::piped())
        .stderr(Stdio::inherit())
        .spawn()
        .expect("spawn graywolf-modem");

    #[cfg(windows)]
    let mut child = Command::new(&bin)
        .stdout(Stdio::piped())
        .stderr(Stdio::inherit())
        .spawn()
        .expect("spawn graywolf-modem");

    let stdout = child.stdout.as_mut().expect("child stdout");
    let mut reader = BufReader::new(stdout);

    #[cfg(unix)]
    {
        let mut one = [0u8; 1];
        reader.read_exact(&mut one).expect("readiness byte");
        assert_eq!(one[0], b'\n');
    }
    #[cfg(windows)]
    let tcp_addr = {
        use std::io::BufRead;
        let mut line = String::new();
        reader.read_line(&mut line).expect("readiness line");
        let port: u16 = line.trim().parse().expect("port number from modem");
        format!("127.0.0.1:{}", port)
    };

    // Connect.
    #[cfg(unix)]
    let mut client = {
        let mut conn = None;
        for _ in 0..50 {
            if let Ok(s) = UnixStream::connect(&sock) {
                conn = Some(s);
                break;
            }
            std::thread::sleep(Duration::from_millis(20));
        }
        let c = conn.expect("connect to modem socket");
        c.set_read_timeout(Some(Duration::from_secs(60))).unwrap();
        c
    };
    #[cfg(windows)]
    let mut client = {
        let c = TcpStream::connect(&tcp_addr).expect("connect TCP");
        c.set_read_timeout(Some(Duration::from_secs(60))).unwrap();
        c
    };

    // ModemReady.
    let ready = read_frame(&mut client).unwrap().unwrap();
    assert!(matches!(ready.payload, Some(Payload::ModemReady(_))));

    // Configure audio: FLAC file source.
    let cfg_audio = IpcMessage {
        payload: Some(Payload::ConfigureAudio(ConfigureAudio {
            device_id: 0,
            device_name: track.to_string_lossy().into(),
            sample_rate: 44100,
            channels: 1,
            source_type: "flac_fast".into(),
            format: "s16le".into(),
            gain_db: 0.0,
        })),
    };
    write_frame(&mut client, &cfg_audio).unwrap();

    // Configure channel. ensemble="" exercises the new default.
    let cfg_chan = IpcMessage {
        payload: Some(Payload::ConfigureChannel(ConfigureChannel {
            channel: 0,
            device_id: 0,
            audio_channel: 0,
            baud: 1200,
            mark_freq: 1200,
            space_freq: 2200,
            modem_type: "afsk".into(),
            profile: "A".into(),
            num_slicers: 1,
            fix_bits: "none".into(),
            num_decoders: 1,
            decoder_offset: 0,
            fx25_encode: false,
            il2p_encode: false,
            input_device_id: 0,
            input_channel: 0,
            output_device_id: 0,
            output_channel: 0,
            demod_ensemble: ensemble.clone(),
        })),
    };
    write_frame(&mut client, &cfg_chan).unwrap();

    // Start.
    let start = IpcMessage {
        payload: Some(Payload::StartAudio(StartAudio {})),
    };
    write_frame(&mut client, &start).unwrap();

    // Collect all frames until the modem goes quiet for 5 seconds
    // (indicating the FLAC source hit EOF and nothing else is decoding).
    let effective_ensemble = if ensemble.is_empty() { "triple (default)" } else { ensemble.as_str() };
    eprintln!(
        "demod-ipc-bench: {} using ensemble={}",
        track.file_name().unwrap().to_string_lossy(),
        effective_ensemble
    );
    let start_time = Instant::now();
    let mut frames = 0u64;
    let mut unique: std::collections::HashSet<Vec<u8>> = std::collections::HashSet::new();
    let quiet_period = Duration::from_secs(5);
    let mut last_frame_at = Instant::now();

    // Short read timeout so we can detect end-of-stream by timeout.
    #[cfg(unix)]
    client
        .set_read_timeout(Some(Duration::from_secs(1)))
        .unwrap();
    #[cfg(windows)]
    client
        .set_read_timeout(Some(Duration::from_secs(1)))
        .unwrap();

    let mut other_msgs = 0u64;
    let mut status_msgs = 0u64;
    let mut dcd_msgs = 0u64;
    let mut level_msgs = 0u64;
    loop {
        // Quiet-period check runs every iteration, not just on read timeouts,
        // because the modem keeps emitting StatusUpdate/DeviceLevelUpdate at
        // ~5 Hz forever — those would mask the "no new frames" signal if we
        // only checked on read-path timeouts.
        if frames > 0 && last_frame_at.elapsed() >= quiet_period {
            break;
        }
        if frames == 0 && start_time.elapsed() >= Duration::from_secs(120) {
            eprintln!("demod-ipc-bench: no frames after 120s, giving up");
            break;
        }

        match read_frame(&mut client) {
            Ok(Some(msg)) => match msg.payload {
                Some(Payload::ReceivedFrame(f)) => {
                    frames += 1;
                    unique.insert(f.data);
                    last_frame_at = Instant::now();
                    if frames == 1 {
                        eprintln!(
                            "demod-ipc-bench: first frame at t={:.2}s",
                            start_time.elapsed().as_secs_f64()
                        );
                    }
                }
                Some(Payload::StatusUpdate(_)) => status_msgs += 1,
                Some(Payload::DcdChange(_)) => dcd_msgs += 1,
                Some(Payload::DeviceLevelUpdate(_)) => level_msgs += 1,
                other => {
                    other_msgs += 1;
                    if other_msgs <= 3 {
                        eprintln!("demod-ipc-bench: other msg: {:?}", other);
                    }
                }
            },
            Ok(None) => break,
            Err(e) => {
                // Read timeout is the common case when audio has ended but
                // status messages are coming slowly; the top-of-loop quiet
                // check handles the exit. Any non-timeout error is terminal.
                if e.kind() != std::io::ErrorKind::WouldBlock
                    && e.kind() != std::io::ErrorKind::TimedOut
                {
                    eprintln!("demod-ipc-bench: read error: {}", e);
                    break;
                }
            }
        }
    }

    let elapsed = start_time.elapsed().as_secs_f64();
    eprintln!(
        "demod-ipc-bench: message counts — frames={}, status={}, dcd={}, level={}, other={}",
        frames, status_msgs, dcd_msgs, level_msgs, other_msgs
    );
    println!(
        "{} packets decoded in {:.3}s (unique={}, ensemble={})",
        frames, elapsed, unique.len(), effective_ensemble
    );

    // Cleanly shut the modem down.
    let _ = child.kill();
    let _ = child.wait();
    #[cfg(unix)]
    let _ = std::fs::remove_file(&sock);
}
