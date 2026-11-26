package bachs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Responses bigger than this are treated as unusable rather than read into memory.
const maxResponseBytes = 10 << 20

type transport struct {
	apiKey     string
	baseURL    *url.URL
	http       *http.Client
	timeout    time.Duration
	maxRetries int
	log        *slog.Logger
}

// RequestOption tunes a single call.
type RequestOption func(*requestOptions)

type requestOptions struct {
	idempotencyKey string
	maxRetries     *int
	timeout        time.Duration
}

// WithIdempotencyKey sends the Idempotency-Key header on a write. Use a key tied
// to the business operation (an order ID, for example) and reuse it only when
// deliberately repeating the same request. Bachs keeps keys for 24 hours.
// It is ignored on reads.
func WithIdempotencyKey(key string) RequestOption {
	return func(o *requestOptions) { o.idempotencyKey = key }
}

// WithRequestMaxRetries overrides the client's retry count for one read.
func WithRequestMaxRetries(n int) RequestOption {
	return func(o *requestOptions) { o.maxRetries = &n }
}

// WithRequestTimeout overrides the client's operation budget for one call.
func WithRequestTimeout(d time.Duration) RequestOption {
	return func(o *requestOptions) { o.timeout = d }
}

var retryStatuses = map[int]bool{429: true, 500: true, 502: true, 503: true, 504: true}

// do sends one operation and decodes a 2xx JSON body into out (if out is non-nil).
// GETs are retried on network errors and retryable statuses; everything else is
// sent exactly once.
func (t *transport) do(ctx context.Context, method, path string, query url.Values, body, out any, opts []RequestOption) error {
	var o requestOptions
	for _, opt := range opts {
		opt(&o)
	}
	write := method != http.MethodGet

	maxRetries := 0
	if !write {
		maxRetries = t.maxRetries
		if o.maxRetries != nil {
			if *o.maxRetries < 0 {
				return &ConfigError{Msg: "max retries cannot be negative"}
			}
			maxRetries = *o.maxRetries
		}
	}
	timeout := t.timeout
	if o.timeout != 0 {
		if o.timeout < 0 {
			return &ConfigError{Msg: "timeout must be positive"}
		}
		timeout = o.timeout
	}
	if write && o.idempotencyKey != "" {
		k := o.idempotencyKey
		if k != strings.TrimSpace(k) || strings.ContainsAny(k, "\r\n") {
			return &ConfigError{Msg: "idempotency key cannot contain surrounding whitespace or line breaks"}
		}
	}

	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return &ConfigError{Msg: "request body must be JSON-serialisable", Err: err}
		}
	}

	// IDs in path were already escaped by idPath, so join the escaped forms.
	u, err := url.Parse(t.baseURL.String() + path)
	if err != nil {
		return &ConfigError{Msg: "could not build request URL", Err: err}
	}
	if len(query) > 0 {
		u.RawQuery = query.Encode()
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	sent := false
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(payload))
		if err != nil {
			return &ConfigError{Msg: "could not build request", Err: err}
		}
		if payload == nil {
			req.Body = nil
		} else {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set("Authorization", "Bearer "+t.apiKey)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "bachs-go/"+Version)
		if write && o.idempotencyKey != "" {
			req.Header.Set("Idempotency-Key", o.idempotencyKey)
		}

		started := time.Now()
		sent = true
		resp, err := t.http.Do(req)
		if err != nil {
			if ctx.Err() == nil && !write && attempt < maxRetries && t.wait(ctx, attempt, "") {
				continue
			}
			// Prefer the context's reason (deadline or caller cancel) when there is one.
			if ctx.Err() != nil {
				err = errors.Join(ctx.Err(), err)
			}
			return &NetworkError{Err: err, OutcomeUnknown: write && sent}
		}

		data, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
		resp.Body.Close()
		requestID := resp.Header.Get("X-Request-Id")
		t.debug(ctx, method, u.Path, resp.StatusCode, attempt, started, requestID)
		if readErr != nil {
			if !write && attempt < maxRetries && ctx.Err() == nil && t.wait(ctx, attempt, "") {
				continue
			}
			return &NetworkError{Err: readErr, OutcomeUnknown: write}
		}

		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			retryAfter := resp.Header.Get("Retry-After")
			if !write && attempt < maxRetries && retryStatuses[resp.StatusCode] && t.wait(ctx, attempt, retryAfter) {
				continue
			}
			return newAPIError(resp.StatusCode, data, requestID, retryAfter, write)
		}

		if len(data) > maxResponseBytes {
			return &ResponseError{StatusCode: resp.StatusCode, RequestID: requestID, Err: errors.New("response body too large")}
		}
		if out == nil {
			return nil
		}
		if err := json.Unmarshal(data, out); err != nil {
			return &ResponseError{StatusCode: resp.StatusCode, RequestID: requestID, Body: data, Err: err}
		}
		return nil
	}
}

// wait sleeps before the next attempt. It returns false when the wait would run
// past the operation's deadline, so the caller should give up instead.
func (t *transport) wait(ctx context.Context, attempt int, retryAfter string) bool {
	d := backoff(attempt, retryAfter)
	if dl, ok := ctx.Deadline(); ok && time.Until(dl) <= d {
		return false
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// backoff is 0.5s, 1s, 2s (capped) with ±25% jitter, or Retry-After if longer.
func backoff(attempt int, retryAfter string) time.Duration {
	base := 500 * time.Millisecond << min(attempt, 3)
	base = min(base, 2*time.Second)
	d := time.Duration(float64(base) * (0.75 + rand.Float64()*0.5))
	return max(d, parseRetryAfter(retryAfter, time.Now()))
}

func parseRetryAfter(v string, now time.Time) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if secs, err := strconv.ParseFloat(v, 64); err == nil && secs >= 0 {
		return time.Duration(secs * float64(time.Second))
	}
	if when, err := http.ParseTime(v); err == nil && when.After(now) {
		return when.Sub(now)
	}
	return 0
}

func (t *transport) debug(ctx context.Context, method, path string, status, attempt int, started time.Time, requestID string) {
	if t.log == nil {
		return
	}
	t.log.DebugContext(ctx, "bachs request",
		"method", method, "path", path, "status", status,
		"attempt", attempt+1, "duration", time.Since(started), "request_id", requestID)
}

// idPath escapes an ID for use as one path segment. "." and ".." are rejected
// because URL resolution would turn them into path navigation and send the
// request, and the API key, to a different endpoint.
func idPath(id, name string) (string, error) {
	if strings.TrimSpace(id) == "" {
		return "", &ConfigError{Msg: name + " must be a non-empty string"}
	}
	if id == "." || id == ".." {
		return "", &ConfigError{Msg: name + " must be an identifier, not a path segment"}
	}
	return url.PathEscape(id), nil
}
