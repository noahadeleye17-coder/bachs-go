// Package webhook verifies Bachs webhook deliveries and, optionally, serves
// them through an http.Handler that drops duplicate deliveries.
//
// Bachs signs each delivery with HMAC-SHA256 over "{timestamp}.{raw body}"
// using the endpoint's signing secret. Delivery is at-least-once and events can
// arrive out of order, so handlers must be idempotent; see Handler and Store.
package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Header names Bachs uses. V2 is preferred; the legacy pair is accepted only
// when V2 is absent.
const (
	HeaderSignatureV2 = "X-Bachs-Signature-V2"
	HeaderSignature   = "X-Bachs-Signature"
	HeaderTimestamp   = "X-Bachs-Timestamp"
)

// DefaultTolerance is how far a delivery's timestamp may be from local time.
const DefaultTolerance = 5 * time.Minute

// Event types Bachs documents. New types may appear; unknown ones still verify.
const (
	CheckoutCompleted           = "checkout.completed"
	CheckoutExpired             = "checkout.expired"
	CollectionSucceeded         = "collection.succeeded"
	CollectionFailed            = "collection.failed"
	CollectionUnderpaid         = "collection.underpaid"
	CustomerSubscriptionCreated = "customer.subscription.created"
	CustomerSubscriptionUpdated = "customer.subscription.updated"
	CustomerSubscriptionDeleted = "customer.subscription.deleted"
	InvoiceCreated              = "invoice.created"
	InvoicePaid                 = "invoice.paid"
	InvoicePaymentFailed        = "invoice.payment_failed"
	PayoutCreated               = "payout.created"
	PayoutPaid                  = "payout.paid"
	PayoutFailed                = "payout.failed"
	PayoutDestinationApproved   = "payout_destination.approved"
	PayoutDestinationRejected   = "payout_destination.rejected"
	RefundCreated               = "refund.created"
	RefundPaid                  = "refund.paid"
	RefundFailed                = "refund.failed"
	DisputeCreated              = "dispute.created"
	DisputeUpdated              = "dispute.updated"
	ConversionCompleted         = "conversion.completed"
	ConversionFailed            = "conversion.failed"
	CustomerCreated             = "customer.created"
	CustomerUpdated             = "customer.updated"
	AccountUpdated              = "account.updated"
	AccountTasksRequested       = "account.tasks_requested"
	AppDeauthorized             = "app.deauthorized"
	CapabilityUpdated           = "capability.updated"
	TransferCreated             = "transfer.created"
)

// Event is the envelope of every delivery.
type Event struct {
	ID             string          `json:"id"` // evt_..., use it to drop duplicates
	Type           string          `json:"type"`
	CreatedAt      string          `json:"created_at"` // compare before overwriting newer state
	OrganizationID string          `json:"organization_id"`
	Account        *string         `json:"account,omitempty"` // set for Connect events
	Data           json.RawMessage `json:"data"`
}

// DecodeData unmarshals the event's data object into v.
func (e *Event) DecodeData(v any) error { return json.Unmarshal(e.Data, v) }

// Errors returned by ConstructEvent. Match them with errors.Is.
var (
	ErrMissingSignature = errors.New("webhook: signature header missing")
	ErrMalformedHeader  = errors.New("webhook: signature header malformed")
	ErrTimestamp        = errors.New("webhook: timestamp outside tolerance")
	ErrSignature        = errors.New("webhook: signature does not match")
	ErrPayload          = errors.New("webhook: verified body is not a valid event")
)

// Option tunes verification.
type Option func(*options)

type options struct {
	tolerance time.Duration
	secrets   []string
	now       func() time.Time
}

// WithTolerance changes the allowed clock difference (default 5 minutes).
// Zero disables the check; only do that in tests.
func WithTolerance(d time.Duration) Option { return func(o *options) { o.tolerance = d } }

// WithAdditionalSecrets accepts more signing secrets, for when you rotate the
// secret on your side and run old and new config side by side.
func WithAdditionalSecrets(secrets ...string) Option {
	return func(o *options) { o.secrets = append(o.secrets, secrets...) }
}

// ConstructEvent checks the signature on the exact bytes received and returns
// the parsed event. Pass the raw body, before any JSON decoding. It does not
// drop duplicates; use Handler with a Store, or check Event.ID yourself.
func ConstructEvent(payload []byte, header http.Header, secret string, opts ...Option) (*Event, error) {
	o := options{tolerance: DefaultTolerance, now: time.Now}
	for _, opt := range opts {
		opt(&o)
	}
	secrets := append([]string{secret}, o.secrets...)
	for _, s := range secrets {
		if s == "" {
			return nil, errors.New("webhook: signing secret is empty")
		}
	}
	if o.tolerance < 0 {
		return nil, errors.New("webhook: tolerance cannot be negative")
	}

	timestamp, signatures, err := parseHeaders(header)
	if err != nil {
		return nil, err
	}
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || ts < 0 || strings.TrimLeft(timestamp, "0123456789") != "" {
		return nil, fmt.Errorf("%w: bad timestamp", ErrMalformedHeader)
	}
	if o.tolerance > 0 {
		skew := o.now().Sub(time.Unix(ts, 0))
		if skew < 0 {
			skew = -skew
		}
		if skew > o.tolerance {
			return nil, ErrTimestamp
		}
	}

	if !verify(payload, timestamp, signatures, secrets) {
		return nil, ErrSignature
	}

	if !utf8.Valid(payload) {
		return nil, fmt.Errorf("%w: not UTF-8", ErrPayload)
	}
	var ev Event
	if err := json.Unmarshal(payload, &ev); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPayload, err)
	}
	if ev.ID == "" || ev.Type == "" || ev.CreatedAt == "" || ev.OrganizationID == "" || !isObject(ev.Data) {
		return nil, fmt.Errorf("%w: missing envelope fields", ErrPayload)
	}
	return &ev, nil
}

// parseHeaders reads V2 ("t=...,v1=...,v1=...") or, only when V2 is absent,
// the legacy timestamp and signature headers. A malformed V2 is an error rather
// than a reason to fall back.
func parseHeaders(h http.Header) (timestamp string, signatures []string, err error) {
	v2, err := single(h, HeaderSignatureV2)
	if err != nil {
		return "", nil, err
	}
	if v2 != "" {
		var stamps []string
		for _, part := range strings.Split(v2, ",") {
			part = strings.TrimSpace(part)
			switch {
			case strings.HasPrefix(part, "t="):
				stamps = append(stamps, part[2:])
			case strings.HasPrefix(part, "v1="):
				signatures = append(signatures, part[3:])
			}
		}
		if len(stamps) != 1 {
			return "", nil, fmt.Errorf("%w: need exactly one timestamp", ErrMalformedHeader)
		}
		if len(signatures) == 0 {
			return "", nil, ErrMissingSignature
		}
		return stamps[0], signatures, nil
	}

	timestamp, err = single(h, HeaderTimestamp)
	if err != nil {
		return "", nil, err
	}
	sig, err := single(h, HeaderSignature)
	if err != nil {
		return "", nil, err
	}
	if timestamp == "" || sig == "" {
		return "", nil, ErrMissingSignature
	}
	return timestamp, []string{sig}, nil
}

func single(h http.Header, name string) (string, error) {
	values := h.Values(name)
	if len(values) > 1 {
		return "", fmt.Errorf("%w: duplicate %s header", ErrMalformedHeader, name)
	}
	if len(values) == 0 {
		return "", nil
	}
	return values[0], nil
}

func verify(payload []byte, timestamp string, signatures, secrets []string) bool {
	ok := false
	for _, secret := range secrets {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(timestamp + "."))
		mac.Write(payload)
		expected := mac.Sum(nil)
		for _, s := range signatures {
			got, err := hex.DecodeString(s)
			if err != nil || len(got) != sha256.Size {
				continue
			}
			// Keep checking after a match so timing does not reveal which one matched.
			if hmac.Equal(expected, got) {
				ok = true
			}
		}
	}
	return ok
}

func isObject(raw json.RawMessage) bool {
	var m map[string]json.RawMessage
	return json.Unmarshal(raw, &m) == nil && m != nil
}

// Sign produces a V2 signature header value. It exists for tests and local
// tools that need to fake a delivery.
func Sign(payload []byte, secret string, t time.Time) string {
	ts := strconv.FormatInt(t.Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "."))
	mac.Write(payload)
	return "t=" + ts + ",v1=" + hex.EncodeToString(mac.Sum(nil))
}
