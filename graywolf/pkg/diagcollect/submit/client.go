package submit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/chrissnell/graywolf/pkg/flareschema"
)

// MaxBodyBytes is the client-side cap on a flare payload (5 MB).
const MaxBodyBytes = 5 * 1024 * 1024

// Client is the abstraction the CLI dispatches to.
type Client interface {
	Submit(body []byte) (flareschema.SubmitResponse, error)
	Update(flareID string, body []byte) (flareschema.SubmitResponse, error)
}

// HTTPClient is the production Client.
type HTTPClient struct {
	baseURL string
	doer    *http.Client
}

// NewHTTPClient. h may be nil; a default with a 30s timeout is used.
func NewHTTPClient(baseURL string, h *http.Client) *HTTPClient {
	if h == nil {
		h = &http.Client{Timeout: 30 * time.Second}
	}
	return &HTTPClient{baseURL: strings.TrimRight(baseURL, "/"), doer: h}
}

func (c *HTTPClient) Submit(body []byte) (flareschema.SubmitResponse, error) {
	return c.post("/api/v1/submit", body)
}

func (c *HTTPClient) Update(flareID string, body []byte) (flareschema.SubmitResponse, error) {
	return c.post(fmt.Sprintf("/api/v1/submit/%s/update", flareID), body)
}

func (c *HTTPClient) post(path string, body []byte) (flareschema.SubmitResponse, error) {
	if len(body) > MaxBodyBytes {
		return flareschema.SubmitResponse{}, ErrPayloadTooLarge{Size: len(body), Max: MaxBodyBytes}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return flareschema.SubmitResponse{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.doer.Do(req)
	if err != nil {
		return flareschema.SubmitResponse{}, fmt.Errorf("post %s: %w", path, err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	switch {
	case resp.StatusCode == http.StatusOK:
		var r flareschema.SubmitResponse
		if err := json.Unmarshal(respBody, &r); err != nil {
			return r, fmt.Errorf("decode 200 body: %w (body=%q)", err, respBody)
		}
		return r, nil
	case resp.StatusCode == http.StatusBadRequest:
		return flareschema.SubmitResponse{}, ErrSchemaRejected{Body: respBody}
	case resp.StatusCode == http.StatusTooManyRequests:
		return flareschema.SubmitResponse{}, ErrRateLimited{
			RetryAfter: resp.Header.Get("Retry-After"),
		}
	case resp.StatusCode >= 500:
		return flareschema.SubmitResponse{}, ErrServerError{Status: resp.StatusCode, Body: respBody}
	default:
		return flareschema.SubmitResponse{}, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, respBody)
	}
}

// ErrSchemaRejected is returned for HTTP 400.
type ErrSchemaRejected struct{ Body []byte }

func (e ErrSchemaRejected) Error() string {
	return fmt.Sprintf("flare-server rejected schema (400): %s", e.Body)
}

// ErrRateLimited is returned for HTTP 429.
type ErrRateLimited struct{ RetryAfter string }

func (e ErrRateLimited) Error() string {
	if e.RetryAfter == "" {
		return "flare-server rate-limited (429)"
	}
	return "flare-server rate-limited (429); retry after " + e.RetryAfter
}

// ErrServerError is returned for HTTP 5xx.
type ErrServerError struct {
	Status int
	Body   []byte
}

func (e ErrServerError) Error() string {
	return fmt.Sprintf("flare-server error %d: %s", e.Status, e.Body)
}

// ErrPayloadTooLarge is returned when body exceeds MaxBodyBytes.
type ErrPayloadTooLarge struct{ Size, Max int }

func (e ErrPayloadTooLarge) Error() string {
	return fmt.Sprintf("flare payload %d bytes exceeds %d-byte cap; reduce log limit or use --no-logs", e.Size, e.Max)
}
