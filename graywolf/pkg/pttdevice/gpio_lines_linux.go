//go:build linux

package pttdevice

import (
	"fmt"

	"github.com/warthog618/go-gpiocdev"
)

// EnumerateGpioLines opens the given gpiochip character device and returns
// information for every line on the chip. The chip is closed before return.
//
// chipPath should be an absolute path to a gpiochip device node (for example,
// "/dev/gpiochip0"). Errors from the kernel are wrapped with chipPath context.
func EnumerateGpioLines(chipPath string) ([]GpioLineInfo, error) {
	chip, err := gpiocdev.NewChip(chipPath)
	if err != nil {
		return nil, fmt.Errorf("enumerate gpio lines on %s: open chip: %w", chipPath, err)
	}
	defer chip.Close()

	count := chip.Lines()
	lines := make([]GpioLineInfo, 0, count)
	for offset := range count {
		info, err := chip.LineInfo(offset)
		if err != nil {
			return nil, fmt.Errorf("enumerate gpio lines on %s: line %d: %w", chipPath, offset, err)
		}
		lines = append(lines, GpioLineInfo{
			Offset:   uint32(info.Offset),
			Name:     info.Name,
			Consumer: info.Consumer,
			Used:     info.Used,
		})
	}
	return lines, nil
}
