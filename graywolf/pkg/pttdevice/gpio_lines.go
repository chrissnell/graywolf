package pttdevice

import "errors"

// ErrNotGpioChip is returned by EnumerateGpioLines when the supplied path
// exists but is not a GPIO character device (e.g., a serial tty). Callers
// map this to a 400 rather than a 500: the caller supplied a bad path.
var ErrNotGpioChip = errors.New("not a gpiochip device")

// GpioLineInfo describes a single GPIO line on a gpiochip character device.
// Returned by EnumerateGpioLines; consumed by the PTT web API to populate the
// GPIO line selector in the UI.
type GpioLineInfo struct {
	// Offset is the 0-indexed line offset within the chip.
	Offset uint32 `json:"offset"`
	// Name is the kernel-assigned line name. May be empty if the line is
	// unnamed on this chip.
	Name string `json:"name"`
	// Consumer is the label of the driver currently holding the line, if any.
	Consumer string `json:"consumer,omitempty"`
	// Used is true when another driver has claimed this line (e.g. SPI, I2C,
	// UART, or a previously-running graywolf process).
	Used bool `json:"used"`
}
