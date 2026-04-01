//! High-performance circular buffer for FIR filter delay lines.
//!
//! Signal processing FIR filters require maintaining a sliding window of recent
//! audio samples. The naive approach shifts the entire array on every sample,
//! which is O(n) per sample. This module provides [`FilterBuf`], which uses a
//! mirroring technique to achieve O(1) push and O(1) contiguous slice access.
//!
//! # Mirroring Technique
//!
//! The backing array is twice the logical capacity. Each sample is written to
//! two positions simultaneously. A decrementing head pointer tracks the newest
//! sample. The slice `data[head..head+capacity]` is always a valid contiguous
//! window with the newest sample at index 0, regardless of wraparound.
//!
//! ```text
//! Logical view:  [newest, ..., oldest]
//! Physical:      [.....primary.....][.....mirror.....]
//!                       ^head              ^head+cap
//! ```

use crate::types::MAX_FILTER_SIZE;

const BUF_CAPACITY: usize = MAX_FILTER_SIZE;
const BUF_LEN: usize = BUF_CAPACITY * 2;

/// Circular buffer optimized for FIR filter convolution.
///
/// Stores the most recent [`MAX_FILTER_SIZE`] samples. Each [`push`](Self::push)
/// is O(1) (two writes), and [`as_slice`](Self::as_slice) returns a contiguous
/// `&[f32]` of the most recent samples in O(1) — no copies or wraparound logic.
///
/// # Usage
///
/// ```
/// use direwolf_demod::filter_buf::FilterBuf;
///
/// let mut buf = FilterBuf::new();
/// buf.push(1.0);
/// buf.push(2.0);
/// buf.push(3.0);
///
/// let s = buf.as_slice();
/// assert_eq!(s[0], 3.0); // newest
/// assert_eq!(s[1], 2.0);
/// assert_eq!(s[2], 1.0); // oldest of the three
/// ```
#[derive(Clone)]
pub struct FilterBuf {
    data: [f32; BUF_LEN],
    head: usize,
}

impl Default for FilterBuf {
    #[inline]
    fn default() -> Self {
        Self::new()
    }
}

impl std::fmt::Debug for FilterBuf {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("FilterBuf")
            .field("head", &self.head)
            .field("capacity", &BUF_CAPACITY)
            .finish_non_exhaustive()
    }
}

impl FilterBuf {
    /// Create a new buffer with all samples initialized to zero.
    #[must_use]
    pub const fn new() -> Self {
        Self {
            data: [0.0; BUF_LEN],
            head: 0,
        }
    }

    /// Push a new sample, which becomes index 0 of [`as_slice`](Self::as_slice).
    #[inline(always)]
    pub fn push(&mut self, val: f32) {
        // Decrement with wraparound: BUF_CAPACITY is not a power of two,
        // so wrapping_sub + modulus would give wrong results.
        self.head = if self.head == 0 { BUF_CAPACITY - 1 } else { self.head - 1 };
        self.data[self.head] = val;
        self.data[self.head + BUF_CAPACITY] = val;
    }

    /// Contiguous slice of the most recent [`MAX_FILTER_SIZE`] samples.
    ///
    /// Index 0 is the newest sample. Callers typically take a sub-slice
    /// of the first `taps` elements for convolution.
    #[inline(always)]
    #[must_use]
    pub fn as_slice(&self) -> &[f32] {
        // SAFETY argument (no unsafe used — bounds are guaranteed):
        // head ∈ [0, BUF_CAPACITY), so head + BUF_CAPACITY ∈ [BUF_CAPACITY, BUF_LEN).
        // The slice [head .. head+BUF_CAPACITY] is always within [0 .. BUF_LEN].
        &self.data[self.head..self.head + BUF_CAPACITY]
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn push_and_read_back() {
        let mut buf = FilterBuf::new();

        buf.push(10.0);
        buf.push(20.0);
        buf.push(30.0);

        let s = buf.as_slice();
        assert_eq!(s[0], 30.0);
        assert_eq!(s[1], 20.0);
        assert_eq!(s[2], 10.0);
    }

    #[test]
    fn wraps_around_correctly() {
        let mut buf = FilterBuf::new();

        for i in 0..(BUF_CAPACITY + 5) {
            buf.push(i as f32);
        }

        let s = buf.as_slice();
        let newest = (BUF_CAPACITY + 4) as f32;
        assert_eq!(s[0], newest);
        assert_eq!(s[1], newest - 1.0);
        assert_eq!(s[BUF_CAPACITY - 1], 5.0);
    }

    #[test]
    fn initial_state_is_zero() {
        let buf = FilterBuf::new();
        let s = buf.as_slice();
        assert!(s.iter().all(|&v| v == 0.0));
    }

    #[test]
    fn convolution_matches_manual() {
        let mut buf = FilterBuf::new();
        let filter = [0.25f32, 0.5, 0.25];

        buf.push(0.0);
        buf.push(1.0);
        buf.push(0.0);

        let s = buf.as_slice();
        let result: f32 = s[..3].iter().zip(&filter).map(|(d, f)| d * f).sum();

        // 0.0*0.25 + 1.0*0.5 + 0.0*0.25 = 0.5
        assert!((result - 0.5).abs() < 1e-6);
    }
}
