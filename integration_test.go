//go:build integration

// Runs against the real Bachs sandbox. It creates a product and a checkout
// session, so use a sandbox key:
//
//	BACHS_API_KEY=sk_sandbox_... go test -tags integration -run Sandbox -v .
package bachs

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

func sandboxClient(t *testing.T) *Client {
	key := os.Getenv("BACHS_API_KEY")
	if key == "" {
		t.Skip("BACHS_API_KEY not set")
	}
	c, err := New(key)
	if err != nil {
		t.Fatal(err)
	}
	if c.Environment != Sandbox {
		t.Fatal("refusing to run integration tests with a live key")
	}
	return c
}

func TestSandboxCheckoutRoundTrip(t *testing.T) {
	c := sandboxClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	run := fmt.Sprintf("bachs-go-it-%d", time.Now().UnixNano())
	product, err := c.Products.Create(ctx, &ProductParams{
		Name:  "bachs-go integration " + run,
		Price: PriceParams{Currency: "USD", Amount: Ptr("5.00")},
	}, WithIdempotencyKey(run+"-product"))
	if err != nil {
		t.Fatal(err)
	}

	created, err := c.CheckoutSessions.Create(ctx, &CheckoutSessionParams{
		ProductCart: []CartItem{{ProductID: product.ID, Quantity: 1}},
		SuccessURL:  "https://example.com/ok",
		Reference:   Ptr(run),
	}, WithIdempotencyKey(run+"-checkout"))
	if err != nil {
		t.Fatal(err)
	}

	got, err := c.CheckoutSessions.Get(ctx, created.CheckoutID)
	if err != nil {
		t.Fatal(err)
	}
	if got.CheckoutID != created.CheckoutID {
		t.Fatalf("got %s, want %s", got.CheckoutID, created.CheckoutID)
	}
	t.Logf("checkout %s: %s", created.CheckoutID, created.CheckoutURL)
}

func TestSandboxReads(t *testing.T) {
	c := sandboxClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if _, err := c.Balances.Get(ctx); err != nil {
		t.Fatal("balances:", err)
	}
	n := 0
	for _, err := range c.Products.ListAll(ctx, &ProductListParams{Limit: 5}) {
		if err != nil {
			t.Fatal("products:", err)
		}
		if n++; n == 12 {
			break
		}
	}
	for _, err := range c.Payments.ListAll(ctx, &PaymentListParams{Limit: 5}) {
		if err != nil {
			t.Fatal("payments:", err)
		}
		break
	}
}
