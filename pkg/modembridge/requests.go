package modembridge

import (
	"context"
	"errors"
	"fmt"
	"time"

	pb "github.com/chrissnell/graywolf/pkg/ipcproto"
)

// EnumerateAudioDevices asks the Rust modem to list available audio devices
// via cpal and waits for the response. Returns nil slice if the bridge is
// not running or the request times out.
func (b *Bridge) EnumerateAudioDevices(ctx context.Context) ([]AvailableDevice, error) {
	if b.State() != StateRunning {
		return nil, errors.New("modembridge: not in RUNNING state")
	}

	reqID, ch := b.enumDispatcher.Register()
	defer b.enumDispatcher.Cancel(reqID)

	msg := &pb.IpcMessage{Payload: &pb.IpcMessage_EnumerateAudioDevices{
		EnumerateAudioDevices: &pb.EnumerateAudioDevices{
			RequestId:     reqID,
			IncludeOutput: true,
		},
	}}
	if err := b.sendIPC(msg); err != nil {
		return nil, fmt.Errorf("send EnumerateAudioDevices: %w", err)
	}

	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case resp := <-ch:
		if resp == nil {
			return nil, errBridgeStopped
		}
		return convertDeviceList(resp), nil
	case <-timer.C:
		return nil, errors.New("modembridge: enumerate timeout")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// ScanInputLevels asks the Rust modem to briefly open each input device,
// measure peak levels, and return the results.
func (b *Bridge) ScanInputLevels(ctx context.Context) ([]InputLevel, error) {
	if b.State() != StateRunning {
		return nil, errors.New("modembridge: not in RUNNING state")
	}

	reqID, ch := b.scanDispatcher.Register()
	defer b.scanDispatcher.Cancel(reqID)

	msg := &pb.IpcMessage{Payload: &pb.IpcMessage_ScanInputLevels{
		ScanInputLevels: &pb.ScanInputLevels{
			RequestId:  reqID,
			DurationMs: 500,
		},
	}}
	if err := b.sendIPC(msg); err != nil {
		return nil, fmt.Errorf("send ScanInputLevels: %w", err)
	}

	timer := time.NewTimer(30 * time.Second) // scanning many devices takes time
	defer timer.Stop()
	select {
	case resp := <-ch:
		if resp == nil {
			return nil, errBridgeStopped
		}
		return convertScanResult(resp), nil
	case <-timer.C:
		return nil, errors.New("modembridge: scan timeout")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// PlayTestTone asks the Rust modem to play a test tone on the named output
// device and waits for the result. Follows the same request/response
// pattern as EnumerateAudioDevices.
func (b *Bridge) PlayTestTone(ctx context.Context, deviceID uint32, deviceName string, sampleRate, channels uint32) error {
	if b.State() != StateRunning {
		return errors.New("modembridge: not in RUNNING state")
	}

	reqID, ch := b.toneDispatcher.Register()
	defer b.toneDispatcher.Cancel(reqID)

	msg := &pb.IpcMessage{Payload: &pb.IpcMessage_PlayTestTone{
		PlayTestTone: &pb.PlayTestTone{
			RequestId:  reqID,
			DeviceName: deviceName,
			SampleRate: sampleRate,
			Channels:   channels,
			DeviceId:   deviceID,
		},
	}}
	if err := b.sendIPC(msg); err != nil {
		return fmt.Errorf("send PlayTestTone: %w", err)
	}

	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case resp := <-ch:
		if resp == nil {
			return errBridgeStopped
		}
		if !resp.Success {
			return fmt.Errorf("test tone failed: %s", resp.Error)
		}
		return nil
	case <-timer.C:
		return errors.New("modembridge: test tone timeout")
	case <-ctx.Done():
		return ctx.Err()
	}
}

// dispatch hooks invoked by dispatchIPC when the matching response
// arrives from the modem.
func (b *Bridge) dispatchEnumResponse(list *pb.AudioDeviceList) {
	b.enumDispatcher.Deliver(list.RequestId, list)
}
func (b *Bridge) dispatchScanResponse(r *pb.InputLevelScanResult) {
	b.scanDispatcher.Deliver(r.RequestId, r)
}
func (b *Bridge) dispatchToneResponse(r *pb.TestToneResult) {
	b.toneDispatcher.Deliver(r.RequestId, r)
}

func convertDeviceList(list *pb.AudioDeviceList) []AvailableDevice {
	out := make([]AvailableDevice, 0, len(list.Devices))
	for _, d := range list.Devices {
		// stable_id is the unique pcm_id (e.g. "hw:CARD=0,DEV=0");
		// fall back to name for non-ALSA platforms.
		path := d.StableId
		if path == "" {
			path = d.Name
		}
		out = append(out, AvailableDevice{
			Name:        d.Name,
			Description: d.Description,
			Path:        path,
			SampleRates: d.SampleRates,
			Channels:    d.ChannelCounts,
			HostAPI:     d.HostApi,
			IsDefault:   d.IsDefault,
			IsInput:     d.Kind == pb.AudioDeviceKind_AUDIO_DEVICE_KIND_INPUT,
			Recommended: d.Recommended,
		})
	}
	return out
}

func convertScanResult(r *pb.InputLevelScanResult) []InputLevel {
	out := make([]InputLevel, 0, len(r.Devices))
	for _, d := range r.Devices {
		out = append(out, InputLevel{
			Name:      d.Name,
			PeakDBFS:  d.PeakDbfs,
			HasSignal: d.HasSignal,
			Error:     d.Error,
		})
	}
	return out
}
