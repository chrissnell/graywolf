package kiss

import (
	"io"
	"testing"
)

func TestSerialConfig_DefaultOpenFuncIsSet(t *testing.T) {
	cfg := SerialConfig{Device: "/dev/null", BaudRate: 9600}
	got := serialOpenOrDefault(cfg)
	if got == nil {
		t.Fatal("serialOpenOrDefault returned nil; default open must be wired")
	}
}

func TestSerialConfig_InjectedOpenFuncWins(t *testing.T) {
	sentinel := io.NopCloser(nil)
	cfg := SerialConfig{
		Device: "x", BaudRate: 1,
		OpenFunc: func(string, uint32) (io.ReadWriteCloser, error) {
			return struct {
				io.ReadWriteCloser
			}{}, nil
		},
	}
	open := serialOpenOrDefault(cfg)
	rwc, err := open("x", 1)
	if err != nil || rwc == nil {
		t.Fatalf("injected OpenFunc not used: rwc=%v err=%v", rwc, err)
	}
	_ = sentinel
}
