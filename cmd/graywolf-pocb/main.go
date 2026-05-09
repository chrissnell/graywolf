// graywolf-pocb is the POC-B Go stub. It connects to the in-process Rust
// modem via UDS, ring-buffers decoded frames, and exposes a tiny
// loopback REST surface for the WebView. Removed in phase 3 once the
// real cmd/graywolf gains an Android entry point.
package main

import (
	"bufio"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	pb "github.com/chrissnell/graywolf/pkg/ipcproto"
)

//go:embed pocb_index.html
var indexHTML []byte

var (
	currentGainMu sync.Mutex
	currentGainDB float64 = -6.0
)

func main() {
	socket := mustEnv("GRAYWOLF_MODEM_SOCKET")
	listen := mustEnv("GRAYWOLF_LISTEN")
	token := mustEnv("GRAYWOLF_LISTEN_TOKEN")

	if sock := os.Getenv("GRAYWOLF_PLATFORM_SOCKET"); sock != "" {
		if err := dialPlatformAndHello(sock); err != nil {
			log.Printf("platformsvc handshake failed: %v", err)
		}
	}

	ring := newFrameRing(50)
	startedAt := time.Now()

	conn, err := dialModem(socket, 10*time.Second)
	if err != nil {
		log.Fatalf("dial modem: %v", err)
	}
	go readModemLoop(conn, ring)

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(indexHTML)
	})
	mux.Handle("/api/_internal/status", bearerAuth(token, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"frames_decoded": ring.count(),
			"uptime_seconds": int(time.Since(startedAt).Seconds()),
		})
	})))
	mux.Handle("/api/_internal/last-frames", bearerAuth(token, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ring.snapshot())
	})))
	mux.Handle("/api/_internal/gain", bearerAuth(token, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			var body struct {
				DB float64 `json:"db"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, "bad json", http.StatusBadRequest)
				return
			}
			currentGainMu.Lock()
			currentGainDB = body.DB
			currentGainMu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		case http.MethodGet:
			currentGainMu.Lock()
			v := currentGainDB
			currentGainMu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]float64{"db": v})
		default:
			http.Error(w, "GET or POST", http.StatusMethodNotAllowed)
		}
	})))

	listener, err := net.Listen("tcp", listen)
	if err != nil {
		log.Fatalf("listen %s: %v", listen, err)
	}

	// Readiness: write \n once both UDS and HTTP listener are up.
	_, _ = os.Stdout.Write([]byte("\n"))
	_ = os.Stdout.Sync()

	log.Printf("graywolf-pocb listening on %s", listen)
	if err := http.Serve(listener, mux); err != nil {
		log.Fatalf("serve: %v", err)
	}
}

func mustEnv(k string) string {
	v := os.Getenv(k)
	if v == "" {
		log.Fatalf("env %s is required", k)
	}
	return v
}

func dialModem(path string, timeout time.Duration) (net.Conn, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		c, err := net.Dial("unix", path)
		if err == nil {
			return c, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil, fmt.Errorf("dial %s: timeout", path)
}

func readModemLoop(conn net.Conn, ring *frameRing) {
	r := bufio.NewReader(conn)
	for {
		msg, err := readFrame(r)
		if err != nil {
			if errors.Is(err, io.EOF) {
				log.Printf("modem ipc closed; exiting")
				os.Exit(1)
			}
			log.Printf("read frame: %v", err)
			os.Exit(1)
		}
		switch p := msg.Payload.(type) {
		case *pb.IpcMessage_ModemReady:
			log.Printf("modem ready version=%s pid=%d", p.ModemReady.Version, p.ModemReady.Pid)
		case *pb.IpcMessage_ReceivedFrame:
			text := formatAX25(p.ReceivedFrame.Data)
			ring.push(decodedLine{
				Stamp: time.Now().UTC().Format(time.RFC3339Nano),
				Text:  text,
			})
		default:
			// Ignore other messages for POC-B (status, dcd, etc.)
		}
	}
}

// formatAX25 renders a UI frame the same way Rust's
// rxonly::format_ax25_ui_frame does, in ~30 lines. Hand-port to keep the
// stub free of Rust dependencies.
func formatAX25(data []byte) string {
	if len(data) < 16 {
		return fmt.Sprintf("(runt %d bytes)", len(data))
	}
	type addrEntry struct {
		call string
		ssid uint8
		hbit bool
	}
	var addrs []addrEntry
	i := 0
	for {
		if i+7 > len(data) {
			return fmt.Sprintf("(malformed @%d)", i)
		}
		chunk := data[i : i+7]
		var call []byte
		for _, b := range chunk[:6] {
			if b&0x01 != 0 {
				return "(bad addr ext)"
			}
			c := b >> 1
			if c != ' ' {
				call = append(call, c)
			}
		}
		ssidByte := chunk[6]
		addrs = append(addrs, addrEntry{string(call), (ssidByte >> 1) & 0x0f, ssidByte&0x80 != 0})
		i += 7
		if ssidByte&0x01 != 0 {
			break
		}
		if len(addrs) > 10 {
			return "(too many addrs)"
		}
	}
	if len(addrs) < 2 || i+2 > len(data) {
		return "(short)"
	}
	info := data[i+2:]
	fmtAddr := func(c string, ssid uint8) string {
		if ssid == 0 {
			return c
		}
		return c + "-" + strconv.Itoa(int(ssid))
	}
	out := fmtAddr(addrs[1].call, addrs[1].ssid) + ">" + fmtAddr(addrs[0].call, addrs[0].ssid)
	for _, d := range addrs[2:] {
		out += "," + fmtAddr(d.call, d.ssid)
		if d.hbit {
			out += "*"
		}
	}
	out += ":" + string(info)
	return out
}
