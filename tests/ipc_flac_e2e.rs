//! End-to-end test: launch the `graywolf-modem` binary, connect to its IPC
//! socket, send ConfigureAudio (flac) + ConfigureChannel + StartAudio, and
//! assert that `ReceivedFrame` messages come back for a known good track.
//!
//! Skipped at runtime if the WB2OSZ test tracks are not present.

use std::io::{BufReader, Read};
use std::os::unix::net::UnixStream;
use std::path::PathBuf;
use std::process::{Command, Stdio};
use std::time::{Duration, Instant};

use direwolf_demod::ipc::framing::{read_frame, write_frame};
use direwolf_demod::ipc::proto::{
    ipc_message::Payload, ConfigureAudio, ConfigureChannel, IpcMessage, StartAudio,
};

fn binary_path() -> PathBuf {
    let mut p = PathBuf::from(env!("CARGO_MANIFEST_DIR"));
    p.push("target");
    p.push(if cfg!(debug_assertions) { "debug" } else { "release" });
    p.push("graywolf-modem");
    p
}

fn test_track() -> Option<PathBuf> {
    let mut p = PathBuf::from(env!("CARGO_MANIFEST_DIR"));
    p.push("aprs-test-tracks");
    p.push("03_100-Mic-E-Bursts-Flat.flac");
    if p.exists() {
        Some(p)
    } else {
        None
    }
}

#[test]
fn flac_end_to_end_yields_frames() {
    let track = match test_track() {
        Some(p) => p,
        None => {
            eprintln!("skipping: aprs-test-tracks not present");
            return;
        }
    };
    let bin = binary_path();
    if !bin.exists() {
        panic!("graywolf-modem binary not built at {}", bin.display());
    }

    let sock = std::env::temp_dir().join(format!(
        "graywolf-e2e-{}-{}.sock",
        std::process::id(),
        Instant::now().elapsed().as_nanos()
    ));
    let _ = std::fs::remove_file(&sock);

    let mut child = Command::new(&bin)
        .arg(&sock)
        .stdout(Stdio::piped())
        .stderr(Stdio::inherit())
        .spawn()
        .expect("spawn graywolf-modem");

    // Wait for readiness byte.
    {
        let stdout = child.stdout.as_mut().expect("child stdout");
        let mut reader = BufReader::new(stdout);
        let mut one = [0u8; 1];
        reader.read_exact(&mut one).expect("readiness byte");
        assert_eq!(one[0], b'\n');
    }

    // Connect.
    let mut client = None;
    for _ in 0..50 {
        if let Ok(s) = UnixStream::connect(&sock) {
            client = Some(s);
            break;
        }
        std::thread::sleep(Duration::from_millis(20));
    }
    let mut client = client.expect("connect to modem socket");
    client
        .set_read_timeout(Some(Duration::from_secs(30)))
        .unwrap();

    // Expect ModemReady first.
    let ready = read_frame(&mut client).unwrap().unwrap();
    assert!(matches!(ready.payload, Some(Payload::ModemReady(_))));

    // Send config.
    let cfg_audio = IpcMessage {
        payload: Some(Payload::ConfigureAudio(ConfigureAudio {
            device_id: 0,
            device_name: track.to_string_lossy().into(),
            sample_rate: 44100,
            channels: 1,
            source_type: "flac".into(),
            format: "s16le".into(),
        })),
    };
    write_frame(&mut client, &cfg_audio).unwrap();

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
        })),
    };
    write_frame(&mut client, &cfg_chan).unwrap();

    let start = IpcMessage {
        payload: Some(Payload::StartAudio(StartAudio {})),
    };
    write_frame(&mut client, &start).unwrap();

    // Read frames until we see at least one ReceivedFrame or hit timeout.
    let mut frames = 0usize;
    let deadline = Instant::now() + Duration::from_secs(60);
    while Instant::now() < deadline && frames == 0 {
        match read_frame(&mut client) {
            Ok(Some(msg)) => match msg.payload {
                Some(Payload::ReceivedFrame(_)) => frames += 1,
                Some(Payload::StatusUpdate(_)) | Some(Payload::DcdChange(_)) => {}
                other => eprintln!("unexpected: {:?}", other),
            },
            Ok(None) => break,
            Err(e) => panic!("read_frame: {}", e),
        }
    }

    // Shutdown.
    let shutdown = IpcMessage {
        payload: Some(Payload::Shutdown(
            direwolf_demod::ipc::proto::Shutdown { timeout_ms: 1000 },
        )),
    };
    let _ = write_frame(&mut client, &shutdown);
    let _ = child.wait();
    let _ = std::fs::remove_file(&sock);

    assert!(frames > 0, "expected at least one decoded frame from track");
}
