//go:build !linux

package pttdevice

func usbInfoFromSysfs(_ string) (vendor, product, description string) { return }
func gpioChipDescription(_ string) string                              { return "" }
