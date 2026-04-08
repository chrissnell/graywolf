//! Dedicated TX worker thread.
//!
//! The IPC handler never blocks on audio drain — it builds samples, pushes
//! a [`TxJob`] onto the worker queue, and returns immediately. The worker
//! owns every [`AudioSink`] and processes one transmission at a time,
//! serializing TX across the whole modem.
//!
//! Serializing through a single worker (rather than one thread per channel
//! plus a per-device mutex) matches the common amateur deployment pattern
//! of one operator / one rig per band and is strictly simpler than
//! direwolf's model. Two channels using *different* output devices will
//! serialize instead of transmitting concurrently; if a future user ever
//! needs that, split this into one worker per output device.

use std::collections::hash_map::Entry;
use std::collections::HashMap;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::mpsc::{channel, RecvTimeoutError, Sender};
use std::sync::Arc;
use std::thread::{self, JoinHandle};
use std::time::{Duration, Instant};

use crate::audio::soundcard::{self, AudioSink, SoundcardOutputConfig};

/// One queued transmission for the worker thread to play.
pub(super) struct TxJob {
    pub channel: u32,
    pub samples: Vec<i16>,
    pub sample_rate: u32,
    pub output_device_id: u32,
    pub sink_config: SoundcardOutputConfig,
}

/// Control message consumed by the worker loop.
enum TxMessage {
    Transmit(TxJob),
    /// Drop every cached output sink. Sent from `stop_all_audio` so a
    /// subsequent reconfigure gets a fresh `spawn_output` on the new
    /// device instead of reusing a stale one.
    ReleaseSinks,
}

/// Handle to the worker thread owned by [`crate::modem::Modem`]. Dropping
/// this releases every cached output device and joins the thread.
pub(super) struct TxWorker {
    sender: Sender<TxMessage>,
    stop: Arc<AtomicBool>,
    join: Option<JoinHandle<()>>,
}

impl TxWorker {
    /// Spawn the worker thread. Returns an error only if the OS refuses
    /// to create the thread.
    pub fn spawn() -> Result<Self, String> {
        let (sender, rx) = channel::<TxMessage>();
        let stop = Arc::new(AtomicBool::new(false));
        let stop_for_thread = stop.clone();

        let join = thread::Builder::new()
            .name("graywolf-tx".into())
            .spawn(move || worker_loop(rx, stop_for_thread))
            .map_err(|e| format!("spawn graywolf-tx thread: {}", e))?;

        Ok(Self {
            sender,
            stop,
            join: Some(join),
        })
    }

    /// Enqueue a transmission. Returns immediately — the actual audio
    /// play-out and PTT sequencing happen on the worker thread.
    pub fn transmit(&self, job: TxJob) -> Result<(), String> {
        self.sender
            .send(TxMessage::Transmit(job))
            .map_err(|e| format!("tx worker transmit: {}", e))
    }

    /// Ask the worker to drop all cached output sinks. Fire-and-forget;
    /// runs after any in-flight transmission completes.
    pub fn release_sinks(&self) {
        let _ = self.sender.send(TxMessage::ReleaseSinks);
    }
}

impl Drop for TxWorker {
    fn drop(&mut self) {
        self.stop.store(true, Ordering::Relaxed);
        if let Some(j) = self.join.take() {
            let _ = j.join();
        }
    }
}

fn worker_loop(rx: std::sync::mpsc::Receiver<TxMessage>, stop: Arc<AtomicBool>) {
    let mut sinks: HashMap<u32, AudioSink> = HashMap::new();
    while !stop.load(Ordering::Relaxed) {
        match rx.recv_timeout(Duration::from_millis(100)) {
            Ok(TxMessage::Transmit(job)) => process_job(&mut sinks, job),
            Ok(TxMessage::ReleaseSinks) => sinks.clear(),
            Err(RecvTimeoutError::Timeout) => continue,
            Err(RecvTimeoutError::Disconnected) => break,
        }
    }
}

fn process_job(sinks: &mut HashMap<u32, AudioSink>, job: TxJob) {
    let TxJob {
        channel,
        samples,
        sample_rate,
        output_device_id,
        sink_config,
    } = job;

    // Lazy-create the sink, holding the &mut returned by the Entry API
    // so the rest of this function never has to look the sink up again.
    let sink = match sinks.entry(output_device_id) {
        Entry::Occupied(e) => e.into_mut(),
        Entry::Vacant(e) => match soundcard::spawn_output(sink_config) {
            Ok(s) => {
                eprintln!(
                    "graywolf-modem: TX sink opened for device_id={} at {} Hz",
                    output_device_id, sample_rate
                );
                e.insert(s)
            }
            Err(err) => {
                eprintln!(
                    "graywolf-modem: TransmitFrame: open output device_id={}: {}",
                    output_device_id, err
                );
                return;
            }
        },
    };

    let n_samples = samples.len();
    let expected = Duration::from_millis((n_samples as u64) * 1000 / sample_rate as u64);

    if let Err(e) = ptt_key(channel) {
        eprintln!("graywolf-modem: TransmitFrame: ptt_key: {}", e);
        return;
    }

    let submit_start = Instant::now();
    let watermark = match sink.submit(samples) {
        Ok(w) => w,
        Err(e) => {
            eprintln!("graywolf-modem: TransmitFrame: sink submit: {}", e);
            if let Err(e) = ptt_unkey(channel) {
                eprintln!("graywolf-modem: TransmitFrame: ptt_unkey: {}", e);
            }
            // A submit error means the sink's background thread died
            // (cpal stream build or play failed after spawn_output
            // returned). Drop the corpse so the next TX gets a fresh
            // attempt instead of bricking the device forever.
            sinks.remove(&output_device_id);
            return;
        }
    };

    // Hybrid drain wait: direwolf's `audio_wait()` alone is documented as
    // "not satisfactory in all cases". On macOS CoreAudio the cpal
    // callback returns before the DAC pipeline has fully played the last
    // samples, so we also block until the expected audio duration has
    // elapsed. Whichever finishes second wins.
    let drain_timeout = expected + Duration::from_millis(500);
    loop {
        let drained_enough = sink.drained_samples() >= watermark;
        let time_elapsed = submit_start.elapsed() >= expected;
        if drained_enough && time_elapsed {
            break;
        }
        if submit_start.elapsed() >= drain_timeout {
            eprintln!(
                "graywolf-modem: TransmitFrame: drain timeout after {} ms ({}/{} samples)",
                drain_timeout.as_millis(),
                sink.drained_samples(),
                n_samples,
            );
            break;
        }
        thread::sleep(Duration::from_millis(5));
    }

    if let Err(e) = ptt_unkey(channel) {
        eprintln!("graywolf-modem: TransmitFrame: ptt_unkey: {}", e);
    }
}

/// Assert PTT for the given channel. Phase C wires this up to the real
/// serial / CM108 / GPIO drivers; until then the audio itself carries
/// the whole transmission (VOX-compatible).
fn ptt_key(_channel: u32) -> Result<(), String> {
    Ok(())
}

/// Release PTT for the given channel. Phase C replaces this stub.
fn ptt_unkey(_channel: u32) -> Result<(), String> {
    Ok(())
}
