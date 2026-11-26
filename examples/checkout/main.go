// Creates a $29 hosted checkout in the sandbox and prints the payment link.
//
//	BACHS_API_KEY=sk_sandbox_... go run ./examples/checkout
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/noahadeleye17-coder/bachs-go"
)

func main() {
	client, err := bachs.New(os.Getenv("BACHS_API_KEY"))
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	orderID := fmt.Sprintf("order-%d", time.Now().Unix())
	session, err := client.CheckoutSessions.Create(ctx, &bachs.CheckoutSessionParams{
		Pricing:    &bachs.CheckoutPricing{Currency: "USD", Amount: bachs.Ptr("29.00")},
		Customer:   &bachs.CustomerInput{Email: "tunde@example.com", Name: "Tunde"},
		SuccessURL: "https://example.com/thanks",
		Reference:  bachs.Ptr(orderID),
		Metadata:   map[string]any{"order_id": orderID},
	}, bachs.WithIdempotencyKey(orderID))

	var apiErr *bachs.APIError
	switch {
	case errors.As(err, &apiErr):
		log.Fatalf("Bachs said no: %s (%s)", apiErr.Detail, apiErr.Code)
	case err != nil:
		log.Fatal(err)
	}

	fmt.Println("checkout:", session.CheckoutID)
	fmt.Println("pay here:", session.CheckoutURL)
	fmt.Println("expires: ", session.ExpiresAt)
}
