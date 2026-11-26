# bachs-go

A Go client for the [Bachs](https://bachs.io) payments API.

> **Community project.** This is not built or supported by Bachs. It follows
> the behaviour of the official [Node.js SDK](https://github.com/bachsdev/bachs-node)
> and the [API docs](https://docs.bachs.io) as closely as I can.

- No dependencies outside the standard library
- Reads retry on 429 and 5xx; writes are never retried behind your back
- Idempotency keys on every write
- `ListAll` iterators that page lazily (Go 1.23 range-over-func)
- Webhook verification, plus an `http.Handler` that drops duplicate deliveries

```sh
go get github.com/noahadeleye17-coder/bachs-go
```

Requires Go 1.23 or newer.

## Quick start

```go
client, err := bachs.New(os.Getenv("BACHS_API_KEY")) // sk_sandbox_... or sk_live_...
if err != nil {
	log.Fatal(err)
}

session, err := client.CheckoutSessions.Create(ctx, &bachs.CheckoutSessionParams{
	Pricing:    &bachs.CheckoutPricing{Currency: "USD", Amount: bachs.Ptr("29.00")},
	SuccessURL: "https://example.com/thanks",
}, bachs.WithIdempotencyKey(orderID))
if err != nil {
	log.Fatal(err)
}
fmt.Println(session.CheckoutURL) // send the buyer here
```

The environment comes from the key prefix. A sandbox key can't reach production
by accident, and `WithEnvironment(bachs.Production)` with a sandbox key fails
before any request goes out.

Money is a decimal string (`"29.00"`), the same as the API. The SDK never turns
it into a `float64`.

Available services: `Products`, `CheckoutSessions`, `Payments`, `Subscriptions`,
`CustomerPortal`, `Balances`, `Banks`, `PayoutDestinations`, `PayoutQuotes`,
`Payouts`. That matches the Node SDK.

## Errors, retries and idempotency

```go
payout, err := client.Payouts.Create(ctx, &bachs.PayoutParams{
	Destination: "dst_123",
	Amount:      "50000.00",
}, bachs.WithIdempotencyKey("payout-"+invoiceID))

var apiErr *bachs.APIError
var netErr *bachs.NetworkError
switch {
case errors.As(err, &apiErr):
	// Bachs answered. apiErr.Code is the stable error code, e.g. INSUFFICIENT_BALANCE.
	if apiErr.OutcomeUnknown { /* 5xx on a write: see below */ }
case errors.As(err, &netErr):
	// Timeout, cancel or connection failure. errors.Is(err, context.DeadlineExceeded) works.
	if netErr.OutcomeUnknown { /* the write may have gone through */ }
}
```

When `OutcomeUnknown` is true, Bachs may have done the work. Look the resource
up first. Only send the write again if it isn't there, and send it with the
**same** idempotency key. Bachs keeps keys for 24 hours.

| | Reads (GET) | Writes |
|---|---|---|
| Retried by the SDK | Up to 2 extra times, on network errors and 429/500/502/503/504 | Never |
| Backoff | 0.5s, 1s, 2s with jitter, or `Retry-After` if longer | n/a |
| Budget | 30s for the whole operation, including waits (`WithTimeout`) | same |

Redirects are never followed, so your key is never sent to another host.

## Pagination

```go
for sub, err := range client.Subscriptions.ListAll(ctx, &bachs.SubscriptionListParams{Status: "past_due"}) {
	if err != nil {
		return err
	}
	fmt.Println(sub.ID, sub.CurrentPeriodEnd)
}
```

Products page by cursor and the rest page by offset. Payout destinations return
`destinations` instead of `items`. `ListAll` handles all three cases for you.
Breaking out of the loop stops further requests. Use `List` when you only need
one page.

## Webhooks

```go
import "github.com/noahadeleye17-coder/bachs-go/webhook"

http.Handle("POST /webhooks/bachs", webhook.Handler(secret,
	func(ctx context.Context, e *webhook.Event) error {
		if e.Type == webhook.CollectionSucceeded {
			// fulfil the order
		}
		return nil // return an error to make Bachs retry
	},
	&webhook.HandlerOptions{Store: myStore},
))
```

`webhook.ConstructEvent(body, r.Header, secret)` checks the signature without
the handler. Pass it the raw body.

What the verifier checks:
- HMAC-SHA256 over `{timestamp}.{raw body}`, compared in constant time
- the `X-Bachs-Signature-V2` header, with any number of `v1=` values (Bachs sends
  more than one while you rotate the secret); the legacy headers are used only
  when V2 is missing
- timestamps more than 5 minutes off are rejected (`WithTolerance`)
- duplicate signature headers and malformed V2 headers are rejected

### Why the handler has a Store

Bachs delivers **at least once** and does not guarantee order. It retries for
about 80 minutes and lets you resend events by hand. So the same event can show
up twice, sometimes at the same moment. Running fulfilment twice means shipping
twice or crediting twice.

The handler claims each event ID in a `Store` before calling your function, and
picks status codes that match Bachs's retry rules:

| Situation | Status | Bachs retries? |
|---|---|---|
| Bad signature or body | 400 | No |
| Already processed | 200 | No |
| Same event still being processed by another request | 503 | Yes |
| Your function or the store failed | 500 | Yes |
| Success | 200 | No |

`webhook.MemoryStore` works for a single instance and for tests. In production,
use your database so the store survives restarts and is shared across
instances. A Postgres version looks like this:

```sql
create table bachs_events (
  event_id   text primary key,
  done       boolean not null default false,
  claimed_at timestamptz not null default now()
);
-- Begin: insert ... on conflict do nothing, then read back done/claimed_at.
-- Finish(ok):  update set done = true.  Finish(failed): delete the row.
```

Bachs waits 10 seconds for a reply. If your work takes longer, queue it and
return.

## Testing

```sh
go test -race ./...

# against the real sandbox (creates a product and a checkout):
BACHS_API_KEY=sk_sandbox_... go test -tags integration -v .
```

The integration tests refuse to run with a live key.

## Differences from the Node SDK

- Errors are values you match with `errors.As` / `errors.Is`, not classes.
- Timeouts and cancellation use `context.Context`. The client timeout is a
  ceiling, so a shorter context deadline wins.
- The webhook package has an optional de-duplicating handler. The Node SDK
  leaves de-duplication to you.
- Responses are decoded into typed structs. Unknown fields are ignored rather
  than kept.

## License

MIT
