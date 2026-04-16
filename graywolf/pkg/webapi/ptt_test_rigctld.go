package webapi

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

// Timeouts for the rigctld probe. dialTimeout bounds the initial TCP
// connect; ioDeadline bounds the full round-trip of the `t` (get_ptt)
// probe after connect. These are intentionally generous compared to the
// steady-state hot-path timeouts in the Rust driver — this is a one-shot
// diagnostic, not the TX critical path.
const (
	rigctldDialTimeout = 3 * time.Second
	rigctldIODeadline  = 2 * time.Second
	rigctldSnippetMax  = 80
)

// handleTestRigctld probes a rigctld endpoint on behalf of the UI's
// "Test Connection" button. It opens a short-lived TCP connection,
// sends `t\n` (get_ptt — non-disruptive, does not key the radio),
// reads the two-line response, and reports success or a diagnostic
// message in the response body.
//
// HTTP conventions (matching sibling handlers in this package):
//   - Malformed JSON body or input validation failure → 400 with a
//     generic {"error": ...} envelope via badRequest. Clients that reach
//     the Test button will never see this in practice — the UI validates
//     first — but server-side defense-in-depth is cheap.
//   - Connection and protocol failures → 200 with
//     TestRigctldResponse{OK:false, Message:"..."}. The response payload,
//     not the HTTP status, is the source of truth for probe outcome.
//   - Success → 200 with TestRigctldResponse{OK:true, LatencyMs:N}.
func (s *Server) handleTestRigctld(w http.ResponseWriter, r *http.Request) {
	req, err := decodeJSON[dto.TestRigctldRequest](r)
	if err != nil {
		badRequest(w, err.Error())
		return
	}

	host := strings.TrimSpace(req.Host)
	if host == "" {
		badRequest(w, "host is required")
		return
	}
	// IPv6 guard. The UI already rejects IPv6, but belt-and-suspenders
	// at the server boundary keeps a malformed client from handing a
	// bracket-less `::1` down to net.JoinHostPort (which would produce
	// an invalid address string for rigctld anyway).
	if strings.ContainsRune(host, ':') {
		badRequest(w, "host must not contain ':' (IPv6 not supported, use hostname or IPv4)")
		return
	}
	if req.Port == 0 {
		// uint16 already caps the upper bound at 65535; 0 is the only
		// out-of-range value to reject here.
		badRequest(w, "port must be in [1, 65535]")
		return
	}

	addr := net.JoinHostPort(host, strconv.Itoa(int(req.Port)))

	start := time.Now()
	conn, err := net.DialTimeout("tcp", addr, rigctldDialTimeout)
	if err != nil {
		s.logger.InfoContext(r.Context(), "rigctld test dial failed", "addr", addr, "err", err)
		writeJSON(w, http.StatusOK, dto.TestRigctldResponse{
			OK:      false,
			Message: "connection failed: " + sanitizeSnippet(err.Error()),
		})
		return
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(rigctldIODeadline)); err != nil {
		s.logger.DebugContext(r.Context(), "rigctld test set deadline failed", "addr", addr, "err", err)
		writeJSON(w, http.StatusOK, dto.TestRigctldResponse{
			OK:      false,
			Message: "set deadline failed: " + sanitizeSnippet(err.Error()),
		})
		return
	}

	if _, err := conn.Write([]byte("t\n")); err != nil {
		writeJSON(w, http.StatusOK, dto.TestRigctldResponse{
			OK:      false,
			Message: "write failed: " + sanitizeSnippet(err.Error()),
		})
		return
	}

	// rigctld's reply to `t` is two lines:
	//   line 1: "0" or "1"       (current PTT state)
	//   line 2: "RPRT <n>"       (0 == success, non-zero == hamlib err code)
	scanner := bufio.NewScanner(conn)
	// Cap the scanner's buffer so a misbehaving server can't balloon us.
	scanner.Buffer(make([]byte, 0, 1024), 4096)

	if !scanner.Scan() {
		writeJSON(w, http.StatusOK, dto.TestRigctldResponse{
			OK:      false,
			Message: rigctldReadErrMessage("read state line", scanner.Err()),
		})
		return
	}
	stateLine := strings.TrimSpace(scanner.Text())
	if stateLine != "0" && stateLine != "1" {
		writeJSON(w, http.StatusOK, dto.TestRigctldResponse{
			OK:      false,
			Message: "unexpected response: " + sanitizeSnippet(stateLine),
		})
		return
	}

	if !scanner.Scan() {
		writeJSON(w, http.StatusOK, dto.TestRigctldResponse{
			OK:      false,
			Message: rigctldReadErrMessage("read RPRT line", scanner.Err()),
		})
		return
	}
	rprtLine := strings.TrimSpace(scanner.Text())
	if !strings.HasPrefix(rprtLine, "RPRT ") {
		writeJSON(w, http.StatusOK, dto.TestRigctldResponse{
			OK:      false,
			Message: "unexpected response: " + sanitizeSnippet(rprtLine),
		})
		return
	}
	codeStr := strings.TrimSpace(strings.TrimPrefix(rprtLine, "RPRT "))
	code, err := strconv.Atoi(codeStr)
	if err != nil {
		writeJSON(w, http.StatusOK, dto.TestRigctldResponse{
			OK:      false,
			Message: "unexpected response: " + sanitizeSnippet(rprtLine),
		})
		return
	}
	if code != 0 {
		// Hamlib error codes are reported as negative integers (e.g.
		// RPRT -6 == "IO error, including open failed"). Surface the
		// raw code so the UI can display it verbatim and users with
		// hamlib experience can look it up.
		writeJSON(w, http.StatusOK, dto.TestRigctldResponse{
			OK:      false,
			Message: fmt.Sprintf("rigctld error: RPRT %d (hamlib code)", code),
		})
		return
	}

	latency := time.Since(start)
	writeJSON(w, http.StatusOK, dto.TestRigctldResponse{
		OK:        true,
		Message:   "Connected",
		LatencyMs: latency.Milliseconds(),
	})
}

// rigctldReadErrMessage builds a diagnostic for a bufio.Scanner read
// failure. Scanner.Err() returns nil at clean EOF, so we surface EOF as
// a distinct message.
func rigctldReadErrMessage(stage string, err error) string {
	if err == nil {
		return stage + ": connection closed before response"
	}
	return stage + ": " + sanitizeSnippet(err.Error())
}

// sanitizeSnippet trims a string to rigctldSnippetMax runes and replaces
// any control characters with '?' so a malicious or buggy server can't
// smuggle terminal escapes or newlines into our JSON response.
func sanitizeSnippet(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	count := 0
	for _, r := range s {
		if count >= rigctldSnippetMax {
			b.WriteString("…")
			break
		}
		if r < 0x20 || r == 0x7f {
			b.WriteRune('?')
		} else {
			b.WriteRune(r)
		}
		count++
	}
	return b.String()
}
