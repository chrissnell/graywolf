package aprs

import (
	"context"
	"log/slog"
)

// LogOutput is a PacketOutput that writes each decoded packet to a slog
// logger at info level. It is the default output wired up in
// cmd/graywolf and is safe for concurrent use.
type LogOutput struct {
	Logger *slog.Logger
}

// NewLogOutput returns a LogOutput that falls back to slog.Default()
// when logger is nil.
func NewLogOutput(logger *slog.Logger) *LogOutput {
	if logger == nil {
		logger = slog.Default()
	}
	return &LogOutput{Logger: logger.With("component", "aprs")}
}

// SendPacket emits a structured log record describing pkt.
func (l *LogOutput) SendPacket(_ context.Context, pkt *DecodedAPRSPacket) error {
	if pkt == nil {
		return nil
	}
	attrs := []any{
		"type", string(pkt.Type),
		"source", pkt.Source,
		"dest", pkt.Dest,
	}
	if len(pkt.Path) > 0 {
		attrs = append(attrs, "path", pkt.Path)
	}
	if pkt.Position != nil {
		attrs = append(attrs,
			"lat", pkt.Position.Latitude,
			"lon", pkt.Position.Longitude,
		)
		if pkt.Position.HasAlt {
			attrs = append(attrs, "alt_m", pkt.Position.Altitude)
		}
		if pkt.Position.HasCourse {
			attrs = append(attrs, "course", pkt.Position.Course, "speed_kt", pkt.Position.Speed)
		}
	}
	if pkt.Message != nil {
		attrs = append(attrs, "to", pkt.Message.Addressee, "text", pkt.Message.Text)
		if pkt.Message.MessageID != "" {
			attrs = append(attrs, "id", pkt.Message.MessageID)
		}
	}
	if pkt.Telemetry != nil {
		attrs = append(attrs, "seq", pkt.Telemetry.Seq)
	}
	if pkt.Weather != nil && pkt.Weather.HasTemp {
		attrs = append(attrs, "tempF", pkt.Weather.Temperature)
	}
	if pkt.Comment != "" {
		attrs = append(attrs, "comment", pkt.Comment)
	}
	l.Logger.Info("aprs packet", attrs...)
	return nil
}

// Close is a no-op; log handlers flush themselves.
func (l *LogOutput) Close() error { return nil }
