package tncprofile

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

type serialRead struct {
	data []byte
	err  error
}

// restartPort keeps reading through the settling interval, so a negative
// RESTART response or a disconnected cable cannot be declared Connected.
// The same reader continues after handoff: no reads are abandoned at timeout
// and every startup KISS byte is delivered to the transport in order.
type restartPort struct {
	io.ReadWriteCloser
	reads      chan serialRead
	stop       chan struct{}
	readerDone chan struct{}
	once       sync.Once
	closeErr   error
	pending    []byte
	readErr    error
}

func newRestartPort(port io.ReadWriteCloser, reader io.Reader) *restartPort {
	p := &restartPort{
		ReadWriteCloser: port,
		reads:           make(chan serialRead, 1),
		stop:            make(chan struct{}),
		readerDone:      make(chan struct{}),
	}
	go func() {
		defer close(p.readerDone)
		emptyReads := 0
		for {
			data := make([]byte, 1024)
			n, err := reader.Read(data)
			if n == 0 && err == nil {
				emptyReads++
				if emptyReads < 100 {
					continue
				}
				err = io.ErrNoProgress
			} else {
				emptyReads = 0
			}
			select {
			case p.reads <- serialRead{data[:n], err}:
			case <-p.stop:
				return
			}
			if err != nil {
				return
			}
		}
	}()
	return p
}

func (p *restartPort) Close() error {
	p.once.Do(func() {
		close(p.stop)
		p.closeErr = p.ReadWriteCloser.Close()
	})
	return p.closeErr
}

func (p *restartPort) Read(dst []byte) (int, error) {
	return p.readContext(context.Background(), dst)
}

// readContext permits bounded initialization waits without abandoning a serial
// read. The one pump remains alive, preserving replies for a retry.
func (p *restartPort) readContext(ctx context.Context, dst []byte) (int, error) {
	if len(dst) == 0 {
		return 0, nil
	}
	for len(p.pending) == 0 {
		if p.readErr != nil {
			return 0, p.readErr
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case next := <-p.reads:
			p.pending, p.readErr = next.data, next.err
		case <-p.stop:
			return 0, io.ErrClosedPipe
		}
	}
	n := copy(dst, p.pending)
	p.pending = p.pending[n:]
	return n, nil
}

func (p *restartPort) waitRestart(ctx context.Context, settle time.Duration) error {
	timer := time.NewTimer(settle)
	defer timer.Stop()
	line := ""
	kissStarted := false
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return ctx.Err()
		case next := <-p.reads:
			if len(p.pending)+len(next.data) > 256*1024 {
				return fmt.Errorf("restart startup data exceeds 256 KiB")
			}
			p.pending = append(p.pending, next.data...)
			for _, b := range next.data {
				if b == 0xc0 {
					kissStarted = true
					line = ""
				}
				if kissStarted {
					continue
				} // KISS payload can contain arbitrary text.
				if b == '\r' || b == '\n' {
					if kenwoodRejected(line) {
						return fmt.Errorf("radio rejected RESTART: %s", strings.TrimSpace(line))
					}
					line = ""
				} else if b >= 32 && b < 127 {
					line += string(b)
					if len(line) > 8192 {
						return fmt.Errorf("restart response exceeds 8192 bytes")
					}
				}
				if strings.HasSuffix(strings.ToLower(line), "cmd:") {
					before := strings.TrimSpace(line[:len(line)-4])
					if kenwoodRejected(before) {
						return fmt.Errorf("radio rejected RESTART: %s", before)
					}
					line = ""
				}
			}
			if next.err != nil {
				p.readErr = next.err
				return fmt.Errorf("restart serial read: %w", next.err)
			}
		}
	}
}
