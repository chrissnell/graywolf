//! Track presentation of queued TX audio, rather than callback consumption.
//!
//! CPAL's playback timestamp predicts when the first frame of a callback
//! reaches the device. Add the position of the last submitted sample in that
//! callback before treating its watermark as played. Idle callbacks must not
//! push that deadline forward. This clock never opens or controls hardware.

use std::sync::Mutex;
use std::time::{Duration, Instant};

#[derive(Default)]
struct Progress {
    rendered: u64,
    ends_at: Option<Instant>,
}

#[derive(Default)]
pub(super) struct PlaybackClock {
    progress: Mutex<Progress>,
}

impl PlaybackClock {
    pub fn rendered(
        &self,
        watermark: u64,
        callback_now: Instant,
        output_latency: Option<Duration>,
        last_frame_end: Duration,
    ) {
        let mut p = self.progress.lock().unwrap_or_else(|e| e.into_inner());
        if watermark <= p.rendered {
            return;
        }
        p.rendered = watermark;
        // Invalid/backwards timestamps are not evidence of completion. The
        // TX worker has a bounded failure path so this cannot leave PTT on.
        p.ends_at = output_latency
            .and_then(|latency| callback_now.checked_add(latency))
            .and_then(|start| start.checked_add(last_frame_end));
    }

    pub fn complete(&self, watermark: u64, now: Instant) -> bool {
        if watermark == 0 {
            return true;
        }
        let p = self.progress.lock().unwrap_or_else(|e| e.into_inner());
        p.rendered >= watermark && p.ends_at.is_some_and(|end| now >= end)
    }

    /// A failed stream discarded its pending audio. Release the waiter;
    /// discarded samples must never be replayed on the replacement stream.
    pub fn discard_through(&self, watermark: u64) {
        let mut p = self.progress.lock().unwrap_or_else(|e| e.into_inner());
        p.rendered = watermark;
        p.ends_at = Some(Instant::now());
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn rendered_is_not_played_with_pch_sized_latency() {
        let c = PlaybackClock::default();
        let now = Instant::now();
        c.rendered(
            48_000,
            now,
            Some(Duration::from_millis(354)),
            Duration::from_millis(341),
        );
        assert!(!c.complete(48_000, now + Duration::from_millis(694)));
        assert!(c.complete(48_000, now + Duration::from_millis(695)));
        assert!(!c.complete(48_001, now + Duration::from_secs(1)));
    }

    #[test]
    fn idle_callbacks_do_not_extend_deadline_and_next_submission_does() {
        let c = PlaybackClock::default();
        let now = Instant::now();
        c.rendered(
            100,
            now,
            Some(Duration::from_millis(20)),
            Duration::from_millis(5),
        );
        c.rendered(
            100,
            now + Duration::from_secs(1),
            Some(Duration::from_secs(1)),
            Duration::from_secs(1),
        );
        assert!(c.complete(100, now + Duration::from_millis(25)));
        assert!(!c.complete(101, now + Duration::from_millis(25)));
        c.rendered(
            101,
            now + Duration::from_secs(1),
            Some(Duration::from_millis(20)),
            Duration::from_millis(1),
        );
        assert!(!c.complete(101, now + Duration::from_millis(1020)));
        assert!(c.complete(101, now + Duration::from_millis(1021)));
    }

    #[test]
    fn missing_timestamp_fails_closed_until_stream_discards_audio() {
        let c = PlaybackClock::default();
        assert!(c.complete(0, Instant::now()));
        c.rendered(10, Instant::now(), None, Duration::ZERO);
        assert!(!c.complete(10, Instant::now() + Duration::from_secs(60)));
        c.discard_through(10);
        assert!(c.complete(10, Instant::now()));
        assert!(!c.complete(11, Instant::now()));
    }
}
