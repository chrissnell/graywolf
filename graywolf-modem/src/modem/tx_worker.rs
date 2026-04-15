//! Dedicated TX worker thread.
//!
//! The IPC handler never blocks on audio drain — it builds samples, pushes
//! a [`TxJob`] onto the worker queue, and returns immediately. The worker
//! owns every [`AudioSink`] and every PTT driver, and processes one
//! transmission at a time, serializing TX across the whole modem.
//!
//! Serializing through a single worker (rather than one thread per channel
//! plus a per-device mutex) matches the common amateur deployment pattern
//! of one operator / one rig per band and is strictly simpler than
//! direwolf's model. Two channels using *different* output devices will
//! serialize instead of transmitting concurrently; if a future user ever
//! needs that, split this into one worker per output device.
//!
//! The worker owns its PTT driver table outright. `ConfigurePtt` on the
//! IPC thread builds the driver (opening any necessary serial port on
//! the IPC thread) and ships it here as a [`TxMessage::RegisterDriver`]
//! — the driver's `Box<dyn PttDriver>` is `Send`, and once it lives in
//! the worker's table only the worker thread ever touches it.

use std::collections::hash_map::Entry;
use std::collections::HashMap;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::mpsc::{channel, RecvTimeoutError, Sender};
use std::sync::Arc;
use std::thread::{self, JoinHandle};
use std::time::{Duration, Instant};

use cpal::Device;

use crate::audio::soundcard::{self, AudioSink, SoundcardOutputConfig};
use crate::tx::ptt::PttDriver;

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
    /// Install (or replace) the PTT driver for a channel. Sent once per
    /// `ConfigurePtt` from the IPC thread; the serial port (if any) is
    /// opened on the IPC thread so the worker never touches hardware
    /// outside of `key`/`unkey`.
    RegisterDriver {
        channel: u32,
        driver: Box<dyn PttDriver>,
    },
    /// Remove the driver for a channel, dropping any cached fd.
    ReleaseDriver {
        channel: u32,
    },
    /// Cache a pre-resolved cpal output device. Sent from `start_audio`
    /// before any input streams are opened, so the enumeration succeeds.
    /// The worker stores it until the first TX on that device_id, then
    /// consumes it to build the [`AudioSink`].
    PrepareOutput {
        device_id: u32,
        device: Device,
    },
    /// Drop every cached output sink. Sent from `stop_all_audio` so a
    /// subsequent reconfigure gets a fresh `spawn_output` on the new
    /// device instead of reusing a stale one.
    ReleaseSinks,
    /// Test-only synchronous query: reply with the number of PTT
    /// drivers currently registered. Because mpsc is FIFO, a successful
    /// reply also proves every earlier message has already been
    /// processed, giving tests a race-free post-condition check.
    #[cfg(test)]
    QueryDriverCount(Sender<usize>),
}

/// Narrow seam over [`AudioSink`] used by [`drive_tx_cycle`]. Keeping
/// the trait at the tx_worker layer (rather than on `AudioSink` itself)
/// means tests exercise the exact production sequencing logic without
/// ever constructing a cpal stream.
pub(super) trait TxSink {
    fn submit(&self, samples: Vec<i16>) -> Result<usize, String>;
    fn drained_samples(&self) -> usize;
}

impl TxSink for AudioSink {
    fn submit(&self, samples: Vec<i16>) -> Result<usize, String> {
        AudioSink::submit(self, samples)
    }

    fn drained_samples(&self) -> usize {
        AudioSink::drained_samples(self)
    }
}

/// Outcome of [`drive_tx_cycle`]. The caller handles logging and sink
/// cleanup based on the variant; `drive_tx_cycle` itself is pure
/// sequence logic so tests can pin the "no spurious sleeps, unkey
/// always runs after key" invariants without touching real hardware.
#[derive(Debug)]
pub(super) enum TxCycleOutcome {
    /// Full cycle completed (either naturally or after drain timeout).
    Done,
    /// PTT key failed — radio was never asserted; sink untouched.
    KeyFailed(String),
    /// Sink submit failed — PTT was released before returning; the
    /// caller should drop the sink because its backing thread is dead.
    SubmitFailed(String),
    /// PTT unkey failed after a successful submit+drain. The radio may
    /// be stuck keyed; the caller should log prominently.
    UnkeyFailed(String),
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

    /// Install or replace the PTT driver for a channel. The driver is
    /// moved to the worker thread and owned there for its lifetime.
    pub fn register_driver(&self, channel: u32, driver: Box<dyn PttDriver>) -> Result<(), String> {
        self.sender
            .send(TxMessage::RegisterDriver { channel, driver })
            .map_err(|e| format!("tx worker register_driver: {}", e))
    }

    /// Drop the PTT driver for a channel. Fire-and-forget.
    pub fn release_driver(&self, channel: u32) {
        let _ = self.sender.send(TxMessage::ReleaseDriver { channel });
    }

    /// Send a pre-resolved cpal output device to the worker. Call this
    /// from `start_audio` before opening any input streams so the device
    /// enumeration runs while the hardware is still available.
    pub fn prepare_output(&self, device_id: u32, device: Device) {
        let _ = self.sender.send(TxMessage::PrepareOutput { device_id, device });
    }

    /// Ask the worker to drop all cached output sinks. Fire-and-forget;
    /// runs after any in-flight transmission completes.
    pub fn release_sinks(&self) {
        let _ = self.sender.send(TxMessage::ReleaseSinks);
    }

    /// Synchronously query the number of PTT drivers the worker thread
    /// currently owns. Used by unit tests to verify that a preceding
    /// `RegisterDriver` / `ReleaseDriver` has been processed. Because
    /// mpsc is FIFO, this also flushes every earlier message before
    /// returning — tests get a race-free barrier for free.
    #[cfg(test)]
    pub(super) fn driver_count(&self) -> usize {
        let (tx, rx) = channel();
        self.sender
            .send(TxMessage::QueryDriverCount(tx))
            .expect("tx worker alive");
        rx.recv().expect("tx worker replies")
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
    let mut drivers: HashMap<u32, Box<dyn PttDriver>> = HashMap::new();
    // Pre-resolved cpal output devices, keyed by device_id. Populated by
    // PrepareOutput before any input streams open; consumed on first TX.
    let mut pending_devices: HashMap<u32, Device> = HashMap::new();
    while !stop.load(Ordering::Relaxed) {
        match rx.recv_timeout(Duration::from_millis(100)) {
            Ok(TxMessage::Transmit(job)) => {
                process_job(&mut sinks, &mut drivers, &mut pending_devices, job);
            }
            Ok(TxMessage::RegisterDriver { channel, driver }) => {
                drivers.insert(channel, driver);
            }
            Ok(TxMessage::ReleaseDriver { channel }) => {
                drivers.remove(&channel);
            }
            Ok(TxMessage::PrepareOutput { device_id, device }) => {
                pending_devices.insert(device_id, device);
            }
            Ok(TxMessage::ReleaseSinks) => {
                sinks.clear();
                pending_devices.clear();
            }
            #[cfg(test)]
            Ok(TxMessage::QueryDriverCount(reply)) => {
                let _ = reply.send(drivers.len());
            }
            Err(RecvTimeoutError::Timeout) => continue,
            Err(RecvTimeoutError::Disconnected) => break,
        }
    }
}

fn process_job(
    sinks: &mut HashMap<u32, AudioSink>,
    drivers: &mut HashMap<u32, Box<dyn PttDriver>>,
    pending_devices: &mut HashMap<u32, Device>,
    job: TxJob,
) {
    let TxJob {
        channel,
        samples,
        sample_rate,
        output_device_id,
        sink_config,
    } = job;

    // Refuse to transmit without a PTT driver registered for the
    // channel — silently keying nothing would be worse than dropping
    // the frame, because the user would have no idea why the radio is
    // deaf. "none" method users still hit this path; they get a
    // NonePtt driver installed by ConfigurePtt and the lookup succeeds.
    let driver = match drivers.get_mut(&channel) {
        Some(d) => d,
        None => {
            eprintln!(
                "graywolf-modem: TransmitFrame: no PTT driver registered for channel {}",
                channel
            );
            return;
        }
    };

    // Lazy-create the sink, holding the &mut returned by the Entry API
    // so the rest of this function never has to look the sink up again.
    //
    // Use a pre-resolved cpal Device if one was sent by PrepareOutput
    // (resolved before input streams opened). Falls back to runtime
    // enumeration if none is cached (error recovery path).
    let sink = match sinks.entry(output_device_id) {
        Entry::Occupied(e) => e.into_mut(),
        Entry::Vacant(e) => {
            let device = pending_devices.remove(&output_device_id);
            match soundcard::spawn_output(sink_config, device) {
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
            }
        }
    };

    // Explicit reborrow so the &mut AudioSink becomes a &AudioSink
    // that drive_tx_cycle can unsize to &dyn TxSink. NLL drops the
    // reborrow after the call returns so the match arms below can
    // mutate `sinks` again.
    let outcome = drive_tx_cycle(driver.as_mut(), &*sink, samples, sample_rate);
    match outcome {
        TxCycleOutcome::Done => {}
        TxCycleOutcome::KeyFailed(e) => {
            eprintln!("graywolf-modem: TransmitFrame: ptt_key: {}", e);
        }
        TxCycleOutcome::SubmitFailed(e) => {
            eprintln!("graywolf-modem: TransmitFrame: sink submit: {}", e);
            // A submit error means the sink's background thread died
            // (cpal stream build or play failed after spawn_output
            // returned). Drop the corpse so the next TX gets a fresh
            // attempt instead of bricking the device forever.
            sinks.remove(&output_device_id);
        }
        TxCycleOutcome::UnkeyFailed(e) => {
            eprintln!("graywolf-modem: TransmitFrame: ptt_unkey: {}", e);
        }
    }
}

/// Run the `key → submit → drain → unkey` sequence against a PTT
/// driver and audio sink. Pure logic with no side effects beyond the
/// injected `driver` and `sink`, so tests can pin the sequencing
/// invariants (call order, no spurious sleeps, PTT released on error)
/// against a mock driver and a fake sink without touching real
/// hardware.
///
/// The caller is responsible for sink lifecycle: [`TxCycleOutcome::SubmitFailed`]
/// is a signal that the sink's backing thread is dead and should be
/// dropped by [`process_job`] so the next TX gets a fresh sink.
fn drive_tx_cycle(
    driver: &mut dyn PttDriver,
    sink: &dyn TxSink,
    samples: Vec<i16>,
    sample_rate: u32,
) -> TxCycleOutcome {
    let n_samples = samples.len();
    // Nanosecond precision so sub-millisecond audio buffers don't
    // truncate to zero in the time-elapsed gate below. sample_rate is
    // u32 from the audio config and is always > 0 in production, but
    // guard anyway so a bad config doesn't divide-by-zero.
    let expected = Duration::from_nanos(
        (n_samples as u64).saturating_mul(1_000_000_000) / sample_rate.max(1) as u64,
    );

    if let Err(e) = driver.key() {
        // Key failed: line was never asserted, nothing to release.
        return TxCycleOutcome::KeyFailed(e);
    }

    let submit_start = Instant::now();
    let watermark = match sink.submit(samples) {
        Ok(w) => w,
        Err(e) => {
            // Submit failed after key(): release PTT before returning
            // so the radio isn't left jamming the band. A failing
            // unkey on top of a failing submit is logged here but we
            // still surface the original submit error to the caller.
            if let Err(ue) = driver.unkey() {
                eprintln!(
                    "graywolf-modem: TransmitFrame: ptt_unkey after submit failure: {}",
                    ue
                );
            }
            return TxCycleOutcome::SubmitFailed(e);
        }
    };

    // Hybrid drain wait: direwolf's `audio_wait()` alone is documented
    // as "not satisfactory in all cases" (xmit.c:885-925). On macOS
    // CoreAudio the cpal callback returns before the DAC pipeline has
    // fully played the last samples, so we also block until the
    // expected audio duration has elapsed. Whichever finishes second
    // wins, bounded by drain_timeout so a dead sink can't wedge us
    // forever.
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

    // No explicit sleep before unkey — `txtail_ms` is already baked
    // into the audio buffer as flag-byte postamble by
    // `tx::build_samples`, and adding a sleep here would double-count
    // the delay. See direwolf `xmit.c:761-936` for the reference
    // sequencing and the "adds no artificial delay" regression test.
    if let Err(e) = driver.unkey() {
        return TxCycleOutcome::UnkeyFailed(e);
    }
    TxCycleOutcome::Done
}

#[cfg(test)]
mod tests {
    //! These tests drive the real [`drive_tx_cycle`] production code
    //! path via a [`MockPtt`] and an in-memory [`InstantDrainSink`].
    //! Because `drive_tx_cycle` is the exact function that
    //! [`process_job`] calls for its sequencing, any regression that
    //! slips a `thread::sleep` between key and submit (or drain and
    //! unkey) will immediately fail the no-delay assertion below.

    use super::*;
    use crate::tx::ptt::tests::{MockPtt, PttCall};
    use std::sync::atomic::AtomicUsize;
    use std::sync::Mutex;

    /// Zero-latency fake that "drains" every submission immediately,
    /// so the drain loop exits on its first check. Optionally fails
    /// on submit to exercise the SubmitFailed error path.
    struct InstantDrainSink {
        submitted: AtomicUsize,
        drained: AtomicUsize,
        fail_submit: bool,
        submit_log: Arc<Mutex<Vec<usize>>>,
    }

    impl InstantDrainSink {
        fn new() -> Self {
            Self {
                submitted: AtomicUsize::new(0),
                drained: AtomicUsize::new(0),
                fail_submit: false,
                submit_log: Arc::new(Mutex::new(Vec::new())),
            }
        }

        fn failing() -> Self {
            Self {
                fail_submit: true,
                ..Self::new()
            }
        }
    }

    impl TxSink for InstantDrainSink {
        fn submit(&self, samples: Vec<i16>) -> Result<usize, String> {
            if self.fail_submit {
                return Err("fake sink submit failure".into());
            }
            let n = samples.len();
            self.submit_log.lock().unwrap().push(n);
            let total = self.submitted.fetch_add(n, Ordering::Relaxed) + n;
            // Advance drained counter in lockstep so the drain loop's
            // `drained_samples() >= watermark` check is satisfied on
            // the first iteration.
            self.drained.fetch_add(n, Ordering::Relaxed);
            Ok(total)
        }

        fn drained_samples(&self) -> usize {
            self.drained.load(Ordering::Relaxed)
        }
    }

    /// PttDriver that always fails on `key()`. Used to verify the
    /// key-failed branch does NOT call unkey (the line was never
    /// asserted, so unkeying would be a lie).
    struct KeyFailingPtt {
        log: Arc<Mutex<Vec<PttCall>>>,
    }

    impl PttDriver for KeyFailingPtt {
        fn key(&mut self) -> Result<(), String> {
            self.log.lock().unwrap().push(PttCall::Key);
            Err("fake key failure".into())
        }

        fn unkey(&mut self) -> Result<(), String> {
            self.log.lock().unwrap().push(PttCall::Unkey);
            Ok(())
        }
    }

    #[test]
    fn drive_tx_cycle_calls_key_submit_then_unkey_in_order() {
        let mock = MockPtt::default();
        let ptt_log = mock.log.clone();
        let sink = InstantDrainSink::new();
        let sink_log = sink.submit_log.clone();
        let mut driver: Box<dyn PttDriver> = Box::new(mock);

        // Non-empty samples so the drain loop actually runs a check.
        match drive_tx_cycle(driver.as_mut(), &sink, vec![0i16; 100], 48000) {
            TxCycleOutcome::Done => {}
            other => panic!("expected Done, got {:?}", other),
        }

        assert_eq!(*ptt_log.lock().unwrap(), vec![PttCall::Key, PttCall::Unkey]);
        assert_eq!(*sink_log.lock().unwrap(), vec![100]);
    }

    #[test]
    fn drive_tx_cycle_adds_no_artificial_delay_between_key_and_unkey() {
        // Empty samples → expected duration = 0 → drain loop exits
        // on the first check. If a future refactor adds a
        // thread::sleep(txdelay_ms) between key and submit — or a
        // thread::sleep(txtail_ms) between drain and unkey — the
        // elapsed time will jump by hundreds of ms and this test
        // will start failing. That's the entire point: txdelay and
        // txtail live in the Phase A audio buffer, not here.
        let sink = InstantDrainSink::new();
        let mut driver: Box<dyn PttDriver> = Box::<MockPtt>::default();

        let start = Instant::now();
        match drive_tx_cycle(driver.as_mut(), &sink, Vec::new(), 48000) {
            TxCycleOutcome::Done => {}
            other => panic!("expected Done, got {:?}", other),
        }
        let elapsed = start.elapsed();

        assert!(
            elapsed < Duration::from_millis(20),
            "cycle took {:?}; suspect an unwanted sleep was added",
            elapsed
        );
    }

    #[test]
    fn drive_tx_cycle_releases_ptt_when_submit_fails() {
        // Real production error path: sink.submit returns Err. The
        // cycle must still call unkey before returning so the radio
        // isn't left jamming the band.
        let mock = MockPtt::default();
        let ptt_log = mock.log.clone();
        let sink = InstantDrainSink::failing();
        let mut driver: Box<dyn PttDriver> = Box::new(mock);

        match drive_tx_cycle(driver.as_mut(), &sink, vec![0i16; 100], 48000) {
            TxCycleOutcome::SubmitFailed(e) => {
                assert!(e.contains("fake sink"), "unexpected error: {}", e);
            }
            other => panic!("expected SubmitFailed, got {:?}", other),
        }

        assert_eq!(
            *ptt_log.lock().unwrap(),
            vec![PttCall::Key, PttCall::Unkey],
            "unkey must run even when submit fails"
        );
    }

    #[test]
    fn drive_tx_cycle_reports_key_failed_and_does_not_unkey() {
        // If key() fails the line was never asserted. Calling unkey
        // anyway would either be a no-op (best case) or actively
        // wrong on some hardware. The cycle must return KeyFailed
        // without touching unkey or the sink.
        let log = Arc::new(Mutex::new(Vec::new()));
        let driver = KeyFailingPtt { log: log.clone() };
        let sink = InstantDrainSink::new();
        let sink_log = sink.submit_log.clone();
        let mut driver: Box<dyn PttDriver> = Box::new(driver);

        match drive_tx_cycle(driver.as_mut(), &sink, vec![0i16; 100], 48000) {
            TxCycleOutcome::KeyFailed(e) => {
                assert!(e.contains("fake key"), "unexpected error: {}", e);
            }
            other => panic!("expected KeyFailed, got {:?}", other),
        }

        assert_eq!(
            *log.lock().unwrap(),
            vec![PttCall::Key],
            "unkey must NOT run when key failed"
        );
        assert!(
            sink_log.lock().unwrap().is_empty(),
            "submit must NOT run when key failed"
        );
    }
}
