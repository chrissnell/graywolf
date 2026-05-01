package webapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

func TestGetMessagesConfig_ReturnsTxChannel(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	ctx := context.Background()
	if err := srv.store.UpsertMessagesConfig(ctx, &configstore.MessagesConfig{TxChannel: 7}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/messages/config", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got dto.MessagesConfig
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.TxChannel != 7 {
		t.Fatalf("TxChannel=%d, want 7", got.TxChannel)
	}
}

func TestPutMessagesConfig_RejectsPacketModeChannel(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	ctx := context.Background()
	// newTestServer's harness seeds an audio device + channel "rx0" at id=1.
	// Add a second packet-mode channel.
	dev, err := srv.store.ListAudioDevices(ctx)
	if err != nil || len(dev) == 0 {
		t.Fatalf("seed precondition: %v / devs=%d", err, len(dev))
	}
	ch := &configstore.Channel{
		Name: "p", InputDeviceID: configstore.U32Ptr(dev[0].ID),
		ModemType: "afsk", BitRate: 1200, MarkFreq: 1200, SpaceFreq: 2200,
		Profile: "A", NumSlicers: 1, FixBits: "none",
		Mode: configstore.ChannelModePacket,
	}
	if err := srv.store.CreateChannel(ctx, ch); err != nil {
		t.Fatalf("create packet channel: %v", err)
	}

	body := fmt.Sprintf(`{"tx_channel":%d}`, ch.ID)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/messages/config", strings.NewReader(body)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s, want 400", rec.Code, rec.Body.String())
	}
}

func TestPutMessagesConfig_RejectsUnknownFields(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	body := bytes.NewBufferString(`{"tx_channel":1,"typo":true}`)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/messages/config", body))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s, want 400", rec.Code, rec.Body.String())
	}
}

// TestPutMessagesConfig_AcceptsAprsModeChannel — sanity check: a normal channel works.
func TestPutMessagesConfig_AcceptsAprsModeChannel(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	body := bytes.NewBufferString(`{"tx_channel":1}`) // rx0 from harness, default aprs mode
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/messages/config", body))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
