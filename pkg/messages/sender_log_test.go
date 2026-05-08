package messages

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/chrissnell/graywolf/pkg/txgovernor"
)

// buildSenderWithLog mirrors buildSender but captures slog output into
// a buffer so tests can assert log lines emitted on TX success/failure.
func buildSenderWithLog(t *testing.T, policy string, rfRunning bool) (*senderRig, *bytes.Buffer) {
	t.Helper()
	rig := buildSender(t, policy, rfRunning)
	buf := &bytes.Buffer{}
	rig.sender.logger = slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
	return rig, buf
}

func TestSender_RF_HappyPath_LogsSentOnHookFire(t *testing.T) {
	rig, buf := buildSenderWithLog(t, FallbackPolicyRFOnly, true)
	defer rig.close()
	row := newOutboundDM(t, rig, "N0CALL", "W1ABC", "hello")

	res := rig.sender.Send(context.Background(), row)
	if res.Err != nil {
		t.Fatalf("Send: %v", res.Err)
	}
	// Submit alone must not produce a "message sent" line — RF success
	// is logged only when the TxHook fires (governor put bytes on the
	// wire), not when the governor merely accepts the queue.
	if strings.Contains(buf.String(), "message sent") {
		t.Errorf("logged sent on Submit (before TxHook):\n%s", buf.String())
	}

	frame := rig.sink.list()[0].Frame
	rig.sender.onTxComplete(7, frame, txgovernor.SubmitSource{Kind: SubmitKindMessages})

	out := buf.String()
	if !strings.Contains(out, "message sent on rf") {
		t.Errorf("expected 'message sent on rf' line, got:\n%s", out)
	}
	if !strings.Contains(out, "channel=7") {
		t.Errorf("expected channel attr, got:\n%s", out)
	}
	if !strings.Contains(out, "to=W1ABC") {
		t.Errorf("expected to attr, got:\n%s", out)
	}
	if !strings.Contains(out, "msg_id="+row.MsgID) {
		t.Errorf("expected msg_id attr, got:\n%s", out)
	}
}

func TestSender_RF_QueueFull_LogsWarn(t *testing.T) {
	rig, buf := buildSenderWithLog(t, FallbackPolicyRFOnly, true)
	defer rig.close()
	row := newOutboundDM(t, rig, "N0CALL", "W1ABC", "hi")
	rig.sink.setErrOnce(txgovernor.ErrQueueFull)

	_ = rig.sender.Send(context.Background(), row)

	out := buf.String()
	if !strings.Contains(out, "level=WARN") {
		t.Errorf("expected WARN level, got:\n%s", out)
	}
	if !strings.Contains(out, "message rf send deferred") {
		t.Errorf("expected deferred line, got:\n%s", out)
	}
	if !strings.Contains(out, "governor queue full") {
		t.Errorf("expected reason, got:\n%s", out)
	}
}

func TestSender_RF_GovernorStopped_LogsWarn(t *testing.T) {
	rig, buf := buildSenderWithLog(t, FallbackPolicyRFOnly, true)
	defer rig.close()
	row := newOutboundDM(t, rig, "N0CALL", "W1ABC", "hi")
	rig.sink.setErr(txgovernor.ErrStopped)

	_ = rig.sender.Send(context.Background(), row)

	out := buf.String()
	if !strings.Contains(out, "message rf send failed") {
		t.Errorf("expected failed line, got:\n%s", out)
	}
	if !strings.Contains(out, "governor stopped") {
		t.Errorf("expected 'governor stopped' reason, got:\n%s", out)
	}
}

func TestSender_RFUnavailable_LogsWarn(t *testing.T) {
	rig, buf := buildSenderWithLog(t, FallbackPolicyRFOnly, false /* bridge not running */)
	defer rig.close()
	row := newOutboundDM(t, rig, "N0CALL", "W1ABC", "hi")

	_ = rig.sender.Send(context.Background(), row)

	out := buf.String()
	if !strings.Contains(out, "message rf send failed") {
		t.Errorf("expected failed line, got:\n%s", out)
	}
	if !strings.Contains(out, "reason=\"rf unavailable\"") {
		t.Errorf("expected 'rf unavailable' reason, got:\n%s", out)
	}
}

func TestSender_IS_HappyPath_LogsSent(t *testing.T) {
	rig, buf := buildSenderWithLog(t, FallbackPolicyISOnly, true)
	defer rig.close()
	row := newOutboundDM(t, rig, "N0CALL", "W1ABC", "hi")

	res := rig.sender.Send(context.Background(), row)
	if res.Err != nil {
		t.Fatalf("Send: %v", res.Err)
	}

	out := buf.String()
	if !strings.Contains(out, "message sent on aprs-is") {
		t.Errorf("expected 'message sent on aprs-is' line, got:\n%s", out)
	}
	if !strings.Contains(out, "to=W1ABC") {
		t.Errorf("expected to attr, got:\n%s", out)
	}
}

func TestSender_LengthGate_LogsWarn(t *testing.T) {
	rig, buf := buildSenderWithLog(t, FallbackPolicyRFOnly, true)
	defer rig.close()
	long := strings.Repeat("x", 300)
	row := newOutboundDM(t, rig, "N0CALL", "W1ABC", long)

	_ = rig.sender.Send(context.Background(), row)

	out := buf.String()
	if !strings.Contains(out, "text too long") {
		t.Errorf("expected length gate Warn, got:\n%s", out)
	}
}

