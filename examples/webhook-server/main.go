// A webhook endpoint that verifies deliveries and drops duplicates.
//
//	BACHS_WEBHOOK_SECRET=... go run ./examples/webhook-server
//
// Expose it with a tunnel (the Bachs CLI or ngrok) and add the public URL as an
// endpoint in the Bachs developer portal.
package main

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"os"

	"github.com/noahadeleye17-coder/bachs-go/webhook"
)

func main() {
	secret := os.Getenv("BACHS_WEBHOOK_SECRET")
	if secret == "" {
		log.Fatal("set BACHS_WEBHOOK_SECRET")
	}

	handle := func(ctx context.Context, e *webhook.Event) error {
		switch e.Type {
		case webhook.CollectionSucceeded:
			var data struct {
				ChargeID   string `json:"charge_id"`
				CheckoutID string `json:"checkout_id"`
				Amount     string `json:"amount"`
				Currency   string `json:"currency"`
			}
			if err := e.DecodeData(&data); err != nil {
				return err
			}
			// Fulfil the order here. Keep it idempotent: checkout.completed can
			// arrive before or after this event.
			log.Printf("paid: %s %s for checkout %s", data.Amount, data.Currency, data.CheckoutID)
		default:
			log.Printf("ignoring %s (%s)", e.Type, e.ID)
		}
		return nil
	}

	mux := http.NewServeMux()
	mux.Handle("POST /webhooks/bachs", webhook.Handler(secret, handle, &webhook.HandlerOptions{
		Store:  &webhook.MemoryStore{}, // use your database in production
		Logger: slog.Default(),
	}))

	log.Println("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}
