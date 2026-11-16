//! Typed errors for the TX path.
//!
//! Phase A only produces one failure mode — an invalid sample rate — but
//! the enum is declared up front so Phase B's IPC handler (which will need
//! to distinguish unknown-channel and missing-output-device errors from
//! DSP-level failures) can grow variants without rippling a bare `String`
//! through the public API.

use std::error::Error;
use std::fmt;

/// Errors returned by the TX path.
#[derive(Debug, Clone, PartialEq, Eq)]
#[non_exhaustive]
pub enum TxError {
    /// `sample_rate` was zero. The NCO phase-increment computation would
    /// divide by zero, so callers must supply a non-zero rate.
    InvalidSampleRate,
    UnsupportedFx25,
    UnsupportedFx25Parity,
    Fx25FrameTooLong,
}

impl fmt::Display for TxError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::UnsupportedFx25Parity => f.write_str("FX.25 parity must be 0 (automatic), 16, 32, or 64"),
            Self::UnsupportedFx25 => f.write_str("FX.25 requires 1200-baud AFSK with 1200/2200 Hz tones"),
            Self::Fx25FrameTooLong => f.write_str("FX.25 frame exceeds the selected standard mode capacity including flags, stuffing and FCS"),
            Self::InvalidSampleRate => f.write_str("sample rate must be non-zero"),
        }
    }
}

impl Error for TxError {}
