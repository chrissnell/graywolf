package packetlog

import (
	"context"
	"testing"
	"time"
)

func TestTXEncodingCorrelatesEitherOrderWithoutDuplicates(t *testing.T) {
	for _, early := range []bool{true, false} {
		l := New(Config{Capacity: 8})
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		ch := l.Subscribe(ctx)
		meta := TXEncoding{Encoding: "fx25", Tag: 9, DataBytes: 191, ParityBytes: 64, FX25Requested: true}
		if early {
			l.SetTXEncoding(7, 2, meta)
		}
		l.Record(Entry{FrameID: 7, Channel: 2, Direction: DirTX})
		if !early {
			l.SetTXEncoding(7, 2, meta)
		}
		rows := l.Query(Filter{Channel: -1})
		if len(rows) != 1 || rows[0].TXEncoding == nil || rows[0].TXEncoding.Tag != 9 {
			t.Fatalf("early=%v: %+v", early, rows)
		}
		l.SetTXEncoding(7, 3, TXEncoding{Encoding: "ax25"})
		l.Record(Entry{FrameID: 8, Channel: 2, Direction: DirTX})
		rows = l.Query(Filter{Channel: -1})
		if rows[1].TXEncoding != nil {
			t.Fatal("metadata leaked to later repeat")
		}
		<-ch
		<-ch
		select {
		case <-ch:
			t.Fatal("metadata generated another packet event")
		default:
		}
	}
}

func TestTXEncodingPendingIsBoundedAndExpires(t *testing.T) {
	l := New(Config{})
	for i := uint64(1); i <= 1000; i++ {
		l.SetTXEncoding(i, 1, TXEncoding{Encoding: "ax25", FallbackReason: "frame_too_large"})
	}
	if len(l.pendingEncoding) > 128 {
		t.Fatal("unbounded reports")
	}
	l.pendingEncoding[txKey{2000, 1}] = pendingEncoding{&TXEncoding{Encoding: "fx25"}, time.Now().Add(-2 * time.Minute)}
	l.Record(Entry{FrameID: 2000, Channel: 1, Direction: DirTX})
	if l.Query(Filter{Channel: -1})[0].TXEncoding != nil {
		t.Fatal("used expired report")
	}
}
