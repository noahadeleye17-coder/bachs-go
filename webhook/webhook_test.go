package webhook

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const secret = "whsec_test"

var body = []byte(`{"id":"evt_1","type":"collection.succeeded","created_at":"2026-11-26T12:00:00Z","organization_id":"acct_1","data":{"charge_id":"ch_1","amount":"29.00","currency":"USD","status":"SUCCEEDED"}}`)

func signed(payload []byte, at time.Time) http.Header {
	h := http.Header{}
	h.Set(HeaderSignatureV2, Sign(payload, secret, at))
	return h
}

func TestValidV2(t *testing.T) {
	ev, err := ConstructEvent(body, signed(body, time.Now()), secret)
	if err != nil {
		t.Fatal(err)
	}
	var data struct {
		ChargeID string `json:"charge_id"`
	}
	if err := ev.DecodeData(&data); err != nil || ev.Type != CollectionSucceeded || data.ChargeID != "ch_1" {
		t.Fatalf("got %+v %+v %v", ev, data, err)
	}
}

func TestRotationAcceptsAnyV1(t *testing.T) {
	h := signed(body, time.Now())
	h.Set(HeaderSignatureV2, h.Get(HeaderSignatureV2)+",v1="+strings.Repeat("ab", 32))
	h.Set(HeaderSignatureV2, strings.Replace(h.Get(HeaderSignatureV2), ",v1=", ",v1="+strings.Repeat("cd", 32)+",v1=", 1))
	if _, err := ConstructEvent(body, h, secret); err != nil {
		t.Fatal(err)
	}
}

func TestLegacyHeaders(t *testing.T) {
	v2 := Sign(body, secret, time.Now())
	parts := strings.Split(v2, ",")
	h := http.Header{}
	h.Set(HeaderTimestamp, strings.TrimPrefix(parts[0], "t="))
	h.Set(HeaderSignature, strings.TrimPrefix(parts[1], "v1="))
	if _, err := ConstructEvent(body, h, secret); err != nil {
		t.Fatal(err)
	}
}

func TestAdditionalSecrets(t *testing.T) {
	h := http.Header{}
	h.Set(HeaderSignatureV2, Sign(body, "whsec_old", time.Now()))
	if _, err := ConstructEvent(body, h, secret, WithAdditionalSecrets("whsec_old")); err != nil {
		t.Fatal(err)
	}
}

func TestRejections(t *testing.T) {
	now := time.Now()
	tampered := []byte(strings.Replace(string(body), "29.00", "2900.00", 1))
	malformedV2 := http.Header{}
	malformedV2.Set(HeaderSignatureV2, "v1=abc")
	dupe := signed(body, now)
	dupe.Add(HeaderSignatureV2, Sign(body, secret, now))
	badJSON := []byte(`{"id":"evt_1"`)
	noData := []byte(`{"id":"evt_1","type":"x","created_at":"t","organization_id":"acct_1","data":null}`)

	cases := []struct {
		name    string
		payload []byte
		header  http.Header
		want    error
	}{
		{"no headers", body, http.Header{}, ErrMissingSignature},
		{"tampered body", tampered, signed(body, now), ErrSignature},
		{"wrong secret", body, func() http.Header { h := http.Header{}; h.Set(HeaderSignatureV2, Sign(body, "nope", now)); return h }(), ErrSignature},
		{"too old", body, signed(body, now.Add(-10*time.Minute)), ErrTimestamp},
		{"from the future", body, signed(body, now.Add(10*time.Minute)), ErrTimestamp},
		{"v2 without timestamp", body, malformedV2, ErrMalformedHeader},
		{"duplicate header", body, dupe, ErrMalformedHeader},
		{"bad json", badJSON, signed(badJSON, now), ErrPayload},
		{"missing data", noData, signed(noData, now), ErrPayload},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ConstructEvent(tc.payload, tc.header, secret)
			if !errors.Is(err, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, err)
			}
		})
	}
}

func TestEmptySecretIsRejected(t *testing.T) {
	if _, err := ConstructEvent(body, signed(body, time.Now()), ""); err == nil {
		t.Fatal("want error")
	}
}

func post(h http.Handler, payload []byte, header http.Header) int {
	req := httptest.NewRequest(http.MethodPost, "/webhooks/bachs", strings.NewReader(string(payload)))
	req.Header = header
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

func TestHandlerDropsDuplicates(t *testing.T) {
	var runs atomic.Int32
	h := Handler(secret, func(ctx context.Context, e *Event) error {
		runs.Add(1)
		return nil
	}, &HandlerOptions{Store: &MemoryStore{}})

	for range 3 {
		if code := post(h, body, signed(body, time.Now())); code != http.StatusOK {
			t.Fatalf("status %d", code)
		}
	}
	if runs.Load() != 1 {
		t.Fatalf("handler ran %d times", runs.Load())
	}
}

func TestHandlerFailureAllowsRetry(t *testing.T) {
	var runs atomic.Int32
	h := Handler(secret, func(ctx context.Context, e *Event) error {
		if runs.Add(1) == 1 {
			return errors.New("db down")
		}
		return nil
	}, &HandlerOptions{Store: &MemoryStore{}})

	if code := post(h, body, signed(body, time.Now())); code != http.StatusInternalServerError {
		t.Fatalf("first delivery status %d", code)
	}
	if code := post(h, body, signed(body, time.Now())); code != http.StatusOK {
		t.Fatalf("retry status %d", code)
	}
	if runs.Load() != 2 {
		t.Fatalf("handler ran %d times", runs.Load())
	}
}

func TestHandlerConcurrentDuplicateGetsRetryableStatus(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	h := Handler(secret, func(ctx context.Context, e *Event) error {
		close(started)
		<-release
		return nil
	}, &HandlerOptions{Store: &MemoryStore{}})

	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); post(h, body, signed(body, time.Now())) }()
	<-started
	if code := post(h, body, signed(body, time.Now())); code != http.StatusServiceUnavailable {
		t.Fatalf("concurrent duplicate status %d, want 503 so Bachs retries", code)
	}
	close(release)
	wg.Wait()
}

func TestHandlerRejectsBadRequests(t *testing.T) {
	h := Handler(secret, func(ctx context.Context, e *Event) error { t.Error("should not run"); return nil }, nil)
	if code := post(h, body, http.Header{}); code != http.StatusBadRequest {
		t.Fatalf("unsigned status %d", code)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET status %d", rec.Code)
	}
	big := make([]byte, MaxBodyBytes+1)
	if code := post(h, big, signed(big, time.Now())); code != http.StatusRequestEntityTooLarge {
		t.Fatalf("big body status %d", code)
	}
}

func TestMemoryStoreExpiresStaleClaims(t *testing.T) {
	now := time.Now()
	m := &MemoryStore{now: func() time.Time { return now }}
	if c, _ := m.Begin(context.Background(), "evt_1"); c != Claimed {
		t.Fatal(c)
	}
	now = now.Add(2 * time.Minute) // the first handler crashed and never finished
	if c, _ := m.Begin(context.Background(), "evt_1"); c != Claimed {
		t.Fatalf("stale claim should be reclaimable, got %v", c)
	}
}
