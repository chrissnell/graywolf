//go:build android

package platformsvc

import (
	"context"
	"errors"
	"testing"
	"time"

	pb "github.com/chrissnell/graywolf/pkg/platformproto"
)

func TestHelloSchemaMismatchReturnsTypedError(t *testing.T) {
	respond := func(req *pb.PlatformMessage) *pb.PlatformMessage {
		// Server claims it speaks schema version 99 — way ahead of the client.
		if req.GetHello() != nil {
			return &pb.PlatformMessage{Body: &pb.PlatformMessage_Hello{
				Hello: &pb.Hello{SchemaVersion: 99, ServerVersion: "v99.0.0"},
			}}
		}
		return nil
	}
	withFakeClient(t, respond, func(c *clientImpl) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, err := c.Hello(ctx, SchemaVersion)
		if err == nil {
			t.Fatal("expected ErrSchemaMismatch, got nil")
		}
		var mm *ErrSchemaMismatch
		if !errors.As(err, &mm) {
			t.Fatalf("expected *ErrSchemaMismatch, got %T: %v", err, err)
		}
		if mm.ClientVersion != SchemaVersion || mm.ServerVersion != 99 {
			t.Errorf("versions: got client=%d server=%d", mm.ClientVersion, mm.ServerVersion)
		}
	})
}

func TestHelloServerErrorReturnsTypedError(t *testing.T) {
	respond := func(req *pb.PlatformMessage) *pb.PlatformMessage {
		if req.GetHello() != nil {
			return &pb.PlatformMessage{Body: &pb.PlatformMessage_Error{
				Error: &pb.Error{Code: pb.ErrorCode_ERROR_SCHEMA_MISMATCH, Message: "go away"},
			}}
		}
		return nil
	}
	withFakeClient(t, respond, func(c *clientImpl) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, err := c.Hello(ctx, SchemaVersion)
		var se *ErrServerError
		if !errors.As(err, &se) {
			t.Fatalf("expected *ErrServerError, got %T: %v", err, err)
		}
		if se.Code != pb.ErrorCode_ERROR_SCHEMA_MISMATCH {
			t.Errorf("code: got %v", se.Code)
		}
	})
}
