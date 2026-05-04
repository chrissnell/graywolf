package actions

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const webhookOutputCap = 1 * 1024

type WebhookExecutor struct {
	client *http.Client
}

func NewWebhookExecutor() *WebhookExecutor {
	return &WebhookExecutor{
		client: &http.Client{
			// Per-call timeout is enforced via the request context (runCtx
			// in Execute). Client.Timeout stays 0 because runCtx already
			// covers dial / TLS handshake / body read.
			Timeout: 0,
			// Refuse to follow redirects. Webhook URLs are operator-set
			// but the response Location is not — an unconstrained 3xx
			// chase to 169.254.169.254 / 127.0.0.1 / RFC1918 hosts would
			// be a SSRF amplifier in a feature triggered by remote APRS
			// callers. Surface the 3xx to the operator instead.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

func (e *WebhookExecutor) Execute(ctx context.Context, req ExecRequest) Result {
	a := req.Action
	if a == nil || a.WebhookURL == "" || a.WebhookMethod == "" {
		return Result{Status: StatusError, StatusDetail: "missing webhook URL/method"}
	}
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	method := strings.ToUpper(a.WebhookMethod)
	rawURL := expandToken(a.WebhookURL, req.Invocation, urlEncoder)
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return Result{Status: StatusError, StatusDetail: "bad URL"}
	}

	var body io.Reader
	contentType := ""
	if method == http.MethodPost {
		if tmpl := strings.TrimSpace(a.WebhookBodyTemplate); tmpl != "" {
			body = strings.NewReader(expandToken(tmpl, req.Invocation, identityEncoder))
		} else {
			form := defaultFormBody(req.Invocation)
			body = strings.NewReader(form)
			contentType = "application/x-www-form-urlencoded"
		}
	}

	httpReq, err := http.NewRequestWithContext(runCtx, method, parsed.String(), body)
	if err != nil {
		return Result{Status: StatusError, StatusDetail: err.Error()}
	}
	if contentType != "" {
		httpReq.Header.Set("Content-Type", contentType)
	}
	headers, herr := decodeHeaders(a.WebhookHeaders)
	if herr != nil {
		return Result{Status: StatusError, StatusDetail: "bad headers JSON"}
	}
	for k, v := range headers {
		httpReq.Header.Set(k, expandToken(v, req.Invocation, identityEncoder))
	}

	resp, doErr := e.client.Do(httpReq)
	if doErr != nil {
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return Result{Status: StatusTimeout, StatusDetail: "timed out"}
		}
		return Result{Status: StatusError, StatusDetail: doErr.Error()}
	}
	defer resp.Body.Close()

	captured, _ := io.ReadAll(io.LimitReader(resp.Body, webhookOutputCap+1))
	if len(captured) > webhookOutputCap {
		captured = captured[:webhookOutputCap]
	}
	httpStatus := resp.StatusCode
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return Result{Status: StatusOK, OutputCapture: string(captured), HTTPStatus: &httpStatus}
	}
	return Result{
		Status:        StatusError,
		StatusDetail:  fmt.Sprintf("http %d", resp.StatusCode),
		OutputCapture: string(captured),
		HTTPStatus:    &httpStatus,
	}
}

func decodeHeaders(s string) (map[string]string, error) {
	if s == "" {
		return nil, nil
	}
	return jsonDecodeMap(s)
}

func defaultFormBody(inv Invocation) string {
	v := url.Values{}
	v.Set("action", inv.ActionName)
	v.Set("sender_callsign", inv.SenderCall)
	v.Set("otp_verified", boolStr(inv.OTPVerified))
	v.Set("otp_cred", inv.OTPCredName)
	v.Set("source", string(inv.Source))
	for _, kv := range inv.Args {
		v.Set(kv.Key, kv.Value)
	}
	return v.Encode()
}

type tokenEncoder func(s string) string

func urlEncoder(s string) string      { return url.QueryEscape(s) }
func identityEncoder(s string) string { return s }

// expandToken substitutes operator-template tokens. Two-stage:
//
//  1. tokenRE matches {{name}} and {{name|filter}}.
//  2. lookupTokenValue resolves the name; applyFilter handles the
//     filter (or the legacy URL-vs-identity baseEncoder if absent).
//
// The single regex pass guarantees substituted output is never
// re-scanned: a value containing literal "{{x}}" does not trigger a
// second substitution. This was load-bearing in the previous
// strings.NewReplacer implementation and remains so.
func expandToken(in string, inv Invocation, enc tokenEncoder) string {
	return expandTokenFiltered(in, inv, enc)
}
