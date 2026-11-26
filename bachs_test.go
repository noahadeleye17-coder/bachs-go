package bachs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

const testKey = "sk_sandbox_test123"

func newTestClient(t *testing.T, h http.HandlerFunc, opts ...Option) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := New(testKey, append([]Option{WithBaseURL(srv.URL)}, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestNewDetectsEnvironment(t *testing.T) {
	c, err := New("sk_live_abc")
	if err != nil || c.Environment != Production {
		t.Fatalf("got %v, %v", c, err)
	}
	c, err = New("sk_sandbox_abc")
	if err != nil || c.Environment != Sandbox {
		t.Fatalf("got %v, %v", c, err)
	}
}

func TestNewRejectsBadConfig(t *testing.T) {
	cases := map[string]struct {
		key  string
		opts []Option
	}{
		"empty key":            {"", nil},
		"whitespace key":       {" sk_sandbox_x", nil},
		"newline key":          {"sk_sandbox_x\n", nil},
		"unknown prefix":       {"pk_test_x", nil},
		"env mismatch":         {"sk_sandbox_x", []Option{WithEnvironment(Production)}},
		"plain http remote":    {"sk_sandbox_x", []Option{WithBaseURL("http://example.com")}},
		"credentials in url":   {"sk_sandbox_x", []Option{WithBaseURL("https://u:p@example.com")}},
		"query in url":         {"sk_sandbox_x", []Option{WithBaseURL("https://example.com?x=1")}},
		"relative url":         {"sk_sandbox_x", []Option{WithBaseURL("/v1")}},
		"negative retries":     {"sk_sandbox_x", []Option{WithMaxRetries(-1)}},
		"zero timeout":         {"sk_sandbox_x", []Option{WithTimeout(0)}},
		"bad environment name": {"sk_sandbox_x", []Option{WithEnvironment("staging")}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := New(tc.key, tc.opts...)
			var cfgErr *ConfigError
			if !errors.As(err, &cfgErr) {
				t.Fatalf("want ConfigError, got %v", err)
			}
		})
	}
}

func TestUnknownPrefixWithExplicitEnvironment(t *testing.T) {
	c, err := New("custom_key", WithEnvironment(Sandbox))
	if err != nil || c.Environment != Sandbox {
		t.Fatalf("got %v, %v", c, err)
	}
}

func TestCreateCheckoutSendsHeadersAndBody(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/checkout-sessions" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+testKey {
			t.Errorf("auth header %q", got)
		}
		if got := r.Header.Get("Idempotency-Key"); got != "order-42" {
			t.Errorf("idempotency key %q", got)
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		pricing := body["pricing"].(map[string]any)
		if pricing["amount"] != "29.00" || pricing["currency"] != "USD" {
			t.Errorf("body %v", body)
		}
		if _, ok := body["product_cart"]; ok {
			t.Error("empty product_cart should be omitted")
		}
		w.Write([]byte(`{"checkout_id":"chk_1","checkout_url":"https://pay.bachs.io/chk_1","status":"open","expires_at":"2026-11-26T13:00:00Z","created_at":"2026-11-26T12:00:00Z","reference":null}`))
	})
	got, err := c.CheckoutSessions.Create(context.Background(), &CheckoutSessionParams{
		Pricing:    &CheckoutPricing{Currency: "USD", Amount: Ptr("29.00")},
		SuccessURL: "https://example.com/ok",
	}, WithIdempotencyKey("order-42"))
	if err != nil {
		t.Fatal(err)
	}
	if got.CheckoutID != "chk_1" || got.Status != "open" {
		t.Fatalf("got %+v", got)
	}
}

func TestCheckoutNeedsExactlyOnePricingSource(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("request should not be sent") })
	for _, p := range []*CheckoutSessionParams{
		nil,
		{},
		{ProductCart: []CartItem{{ProductID: "prod_1"}}, Pricing: &CheckoutPricing{Currency: "USD"}},
	} {
		if _, err := c.CheckoutSessions.Create(context.Background(), p); err == nil {
			t.Errorf("want error for %+v", p)
		}
	}
}

func TestReadsRetryOnRetryableStatus(t *testing.T) {
	var calls atomic.Int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte(`{"id":"sub_1","status":"active"}`))
	})
	sub, err := c.Subscriptions.Get(context.Background(), "sub_1")
	if err != nil {
		t.Fatal(err)
	}
	if sub.Status != SubscriptionActive || calls.Load() != 3 {
		t.Fatalf("status %q after %d calls", sub.Status, calls.Load())
	}
}

func TestReadsGiveUpAfterMaxRetries(t *testing.T) {
	var calls atomic.Int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}, WithMaxRetries(1))
	_, err := c.Payments.Get(context.Background(), "pay_1")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 502 || apiErr.OutcomeUnknown {
		t.Fatalf("got %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("want 2 calls, got %d", calls.Load())
	}
}

func TestWritesAreNeverRetried(t *testing.T) {
	var calls atomic.Int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"detail":"try later","error_code":"SERVICE_UNAVAILABLE"}`))
	})
	_, err := c.Payouts.Create(context.Background(), &PayoutParams{Destination: "dst_1", Amount: "100.00"}, WithIdempotencyKey("p-1"))
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("got %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("write sent %d times", calls.Load())
	}
	if !apiErr.OutcomeUnknown || apiErr.Code != "SERVICE_UNAVAILABLE" {
		t.Fatalf("got %+v", apiErr)
	}
}

func TestAPIErrorFields(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-Id", "req_9")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"detail":"bad email","error_code":"VALIDATION_ERROR","doc_url":"https://docs.bachs.io/errors","errors":[{"field":"customer.email","message":"invalid","code":"invalid_email"}]}`))
	})
	_, err := c.CheckoutSessions.Create(context.Background(), &CheckoutSessionParams{Pricing: &CheckoutPricing{Currency: "USD", Amount: Ptr("1.00")}})
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatal(err)
	}
	if apiErr.Code != "VALIDATION_ERROR" || apiErr.RequestID != "req_9" || len(apiErr.FieldErrors) != 1 || apiErr.FieldErrors[0].Field != "customer.email" {
		t.Fatalf("got %+v", apiErr)
	}
	if apiErr.OutcomeUnknown {
		t.Fatal("a 4xx write has a known outcome")
	}
}

func TestNonJSONSuccessIsResponseError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("<html>")) })
	_, err := c.Balances.Get(context.Background())
	var respErr *ResponseError
	if !errors.As(err, &respErr) {
		t.Fatalf("got %v", err)
	}
}

func TestRedirectsAreNotFollowed(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://evil.example.com/steal", http.StatusFound)
	})
	_, err := c.Balances.Get(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusFound {
		t.Fatalf("got %v", err)
	}
}

func TestTimeoutIsNetworkError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}, WithTimeout(50*time.Millisecond))
	_, err := c.Payouts.Create(context.Background(), &PayoutParams{Destination: "dst_1", QuoteID: "q_1"})
	var netErr *NetworkError
	if !errors.As(err, &netErr) || !netErr.OutcomeUnknown || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v", err)
	}
}

func TestIDsAreEscapedAndPathTricksRejected(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/v1/payments/a%2Fb" {
			t.Errorf("path %q", r.URL.EscapedPath())
		}
		w.Write([]byte(`{"payment_id":"a/b","status":"succeeded","amount":"1.00","currency":"USD"}`))
	})
	if _, err := c.Payments.Get(context.Background(), "a/b"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"", " ", ".", ".."} {
		if _, err := c.Payments.Get(context.Background(), id); err == nil {
			t.Errorf("want error for %q", id)
		}
	}
}

func TestProductsListAllFollowsCursor(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("cursor") {
		case "":
			w.Write([]byte(`{"items":[{"id":"prod_1"},{"id":"prod_2"}],"pagination":{"next_cursor":"c2"}}`))
		case "c2":
			w.Write([]byte(`{"items":[{"id":"prod_3"}],"pagination":{"next_cursor":null}}`))
		}
	})
	var ids []string
	for p, err := range c.Products.ListAll(context.Background(), &ProductListParams{Limit: 2}) {
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, p.ID)
	}
	if fmt.Sprint(ids) != "[prod_1 prod_2 prod_3]" {
		t.Fatalf("got %v", ids)
	}
}

func TestOffsetListAllStopsOnTotalAndBreak(t *testing.T) {
	var calls atomic.Int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		if offset >= 3 {
			t.Errorf("fetched past total, offset %d", offset)
		}
		items := `[{"id":"po_a","status":"completed","amount":"1","currency":"NGN"},{"id":"po_b","status":"completed","amount":"1","currency":"NGN"}]`
		if offset == 2 {
			items = `[{"id":"po_c","status":"completed","amount":"1","currency":"NGN"}]`
		}
		fmt.Fprintf(w, `{"total":3,"items":%s}`, items)
	})
	n := 0
	for _, err := range c.Payouts.ListAll(context.Background(), nil) {
		if err != nil {
			t.Fatal(err)
		}
		n++
	}
	if n != 3 || calls.Load() != 2 {
		t.Fatalf("got %d items over %d calls", n, calls.Load())
	}

	calls.Store(0)
	for range c.Payouts.ListAll(context.Background(), nil) {
		break
	}
	if calls.Load() != 1 {
		t.Fatalf("breaking early should stop paging, made %d calls", calls.Load())
	}
}

func TestDestinationsListAllReadsDestinationsField(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"destinations":[{"id":"dst_1","name":"GTBank","type":"bank","currency":"NGN","status":"approved"}],"total":1,"limit":20,"offset":0}`))
	})
	var got []string
	for d, err := range c.PayoutDestinations.ListAll(context.Background(), nil) {
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, d.ID)
	}
	if fmt.Sprint(got) != "[dst_1]" {
		t.Fatalf("got %v", got)
	}
}

func TestListAllYieldsErrorOnce(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"error_code":"UNAUTHORIZED"}`)
	})
	errs := 0
	for _, err := range c.Payments.ListAll(context.Background(), nil) {
		if err != nil {
			errs++
		}
	}
	if errs != 1 {
		t.Fatalf("want one error, got %d", errs)
	}
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	if d := parseRetryAfter("2", now); d != 2*time.Second {
		t.Fatal(d)
	}
	if d := parseRetryAfter(now.Add(3*time.Second).Format(http.TimeFormat), now); d != 3*time.Second {
		t.Fatal(d)
	}
	if d := parseRetryAfter("soon", now); d != 0 {
		t.Fatal(d)
	}
}
