//go:build !linux

package pttdevice

import "fmt"

// EnumerateGpioLines returns an error on non-Linux platforms; gpiochip is a
// Linux kernel API.
func EnumerateGpioLines(_ string) ([]GpioLineInfo, error) {
	return nil, fmt.Errorf("gpio line enumeration is only supported on Linux")
}
