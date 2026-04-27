// Package mapsauth is a thin client for auth.nw5w.com, the registration
// endpoint that issues per-device tokens for the Graywolf private map
// service. The package exists so handlers in pkg/webapi don't have to
// hand-roll JSON+timeout+error-mapping code, and so tests can swap in
// an httptest.Server via the BaseURL constructor argument.
package mapsauth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultBaseURL is the production registration endpoint. Override
// via NewClient when stubbing in tests.
const DefaultBaseURL = "https://auth.nw5w.com"

// Client is the registration client. Construct via NewClient.
type Client struct {
	baseURL string
	http    *http.Client
}

// NewClient returns a client pointed at baseURL. Pass DefaultBaseURL
// in production. The HTTP client has a 15-second total timeout — the
// auth worker is fast under normal load, and a slow server is more
// likely a routing problem than a real deferred response.
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 15 * time.Second},
	}
}

// RegisterResponse is the success body emitted by POST /register.
type RegisterResponse struct {
	Callsign string `json:"callsign"`
	Token    string `json:"token"`
}

// Error wraps a non-2xx response from the auth server. Status holds
// the HTTP status; Code is the machine-readable error name from the
// response body (or a synthesized value for empty-body cases like
// 429); Message is the human-readable string the server returned and
// is intended to be surfaced to the user verbatim — it points them
// at https://github.com/chrissnell/graywolf/issues.
type Error struct {
	Status  int    `json:"-"`
	Code    string `json:"error"`
	Message string `json:"message"`
}

func (e *Error) Error() string {
	return fmt.Sprintf("mapsauth: %s (status %d): %s", e.Code, e.Status, e.Message)
}

// Register POSTs the callsign to /register and returns the issued
// token on success. callsign should already be uppercased and have
// any -SSID stripped; the server will reject anything that doesn't
// match ^[A-Z0-9]{3,9}$ with a digit.
func (c *Client) Register(ctx context.Context, callsign string) (RegisterResponse, error) {
	body, err := json.Marshal(map[string]string{"callsign": callsign})
	if err != nil {
		return RegisterResponse{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/register", bytes.NewReader(body))
	if err != nil {
		return RegisterResponse{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return RegisterResponse{}, err
	}
	defer resp.Body.Close()
	// Cap the read at 64 KiB. Legitimate success bodies are ~100 bytes;
	// the largest documented error body is the issue-tracker message at
	// ~110 bytes. 64 KiB leaves headroom for HTML 5xx pages from a
	// misbehaving edge while preventing a hostile server from OOMing us.
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if readErr != nil {
		return RegisterResponse{}, fmt.Errorf("mapsauth: read body: %w", readErr)
	}

	if resp.StatusCode == http.StatusOK {
		var out RegisterResponse
		if err := json.Unmarshal(raw, &out); err != nil {
			return RegisterResponse{}, fmt.Errorf("mapsauth: decode success body: %w", err)
		}
		return out, nil
	}

	// 429 has an empty body per the integration doc; synthesize.
	if resp.StatusCode == http.StatusTooManyRequests {
		return RegisterResponse{}, &Error{
			Status:  resp.StatusCode,
			Code:    "rate_limited",
			Message: "Too many registration attempts. Please wait a moment and try again.",
		}
	}

	rerr := &Error{Status: resp.StatusCode}
	if err := json.Unmarshal(raw, rerr); err != nil || rerr.Code == "" {
		// 5xx with HTML, or unrecognized body — synthesize.
		rerr.Code = "internal"
		rerr.Message = "Registration failed. Please file an issue at https://github.com/chrissnell/graywolf/issues"
	}
	return RegisterResponse{}, rerr
}
