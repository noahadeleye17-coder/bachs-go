package bachs

import (
	"context"
	"iter"
	"net/http"
	"net/url"
	"strconv"
)

func limitOffset(q url.Values, limit, offset int) {
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	if offset > 0 {
		q.Set("offset", strconv.Itoa(offset))
	}
}

func setIf(q url.Values, key, value string) {
	if value != "" {
		q.Set(key, value)
	}
}

// send runs one operation and returns the decoded body, or nil and an error.
func send[T any](ctx context.Context, t *transport, method, path string, q url.Values, body any, opts []RequestOption) (*T, error) {
	out := new(T)
	if err := t.do(ctx, method, path, q, body, out, opts); err != nil {
		return nil, err
	}
	return out, nil
}

// ---- Products ----

type ProductsService struct{ t *transport }

// Create makes a product. A product with a BillingCycle is recurring.
func (s *ProductsService) Create(ctx context.Context, params *ProductParams, opts ...RequestOption) (*Product, error) {
	if params == nil {
		return nil, &ConfigError{Msg: "product params are required"}
	}
	return send[Product](ctx, s.t, http.MethodPost, "/v1/products", nil, params, opts)
}

func (s *ProductsService) Get(ctx context.Context, id string, opts ...RequestOption) (*Product, error) {
	p, err := idPath(id, "product ID")
	if err != nil {
		return nil, err
	}
	return send[Product](ctx, s.t, http.MethodGet, "/v1/products/"+p, nil, nil, opts)
}

// List returns one page of products.
func (s *ProductsService) List(ctx context.Context, params *ProductListParams, opts ...RequestOption) (*ProductList, error) {
	q := url.Values{}
	if params != nil {
		limitOffset(q, params.Limit, 0)
		setIf(q, "cursor", params.Cursor)
		if params.IncludeArchived {
			q.Set("include_archived", "true")
		}
	}
	return send[ProductList](ctx, s.t, http.MethodGet, "/v1/products", q, nil, opts)
}

// ListAll walks every product, following next_cursor. params.Cursor is ignored.
func (s *ProductsService) ListAll(ctx context.Context, params *ProductListParams, opts ...RequestOption) iter.Seq2[Product, error] {
	base := ProductListParams{}
	if params != nil {
		base = *params
	}
	return cursorPages(ctx, func(ctx context.Context, cursor string) (page[Product], error) {
		p := base
		p.Cursor = cursor
		list, err := s.List(ctx, &p, opts...)
		if err != nil {
			return page[Product]{}, err
		}
		next := ""
		if list.Pagination.NextCursor != nil {
			next = *list.Pagination.NextCursor
		}
		return page[Product]{items: list.Items, next: next}, nil
	})
}

// ---- Checkout sessions ----

type CheckoutSessionsService struct{ t *transport }

// Create starts a hosted checkout. Redirect the buyer to CheckoutURL, then
// fulfil the order from the checkout.completed / collection.succeeded
// webhooks, never from the success redirect.
func (s *CheckoutSessionsService) Create(ctx context.Context, params *CheckoutSessionParams, opts ...RequestOption) (*CheckoutSessionCreated, error) {
	if params == nil || (len(params.ProductCart) > 0) == (params.Pricing != nil) {
		return nil, &ConfigError{Msg: "set exactly one of ProductCart or Pricing"}
	}
	return send[CheckoutSessionCreated](ctx, s.t, http.MethodPost, "/v1/checkout-sessions", nil, params, opts)
}

func (s *CheckoutSessionsService) Get(ctx context.Context, id string, opts ...RequestOption) (*CheckoutSession, error) {
	p, err := idPath(id, "checkout ID")
	if err != nil {
		return nil, err
	}
	return send[CheckoutSession](ctx, s.t, http.MethodGet, "/v1/checkout-sessions/"+p, nil, nil, opts)
}

// ---- Payments ----

type PaymentsService struct{ t *transport }

func (s *PaymentsService) Get(ctx context.Context, id string, opts ...RequestOption) (*Payment, error) {
	p, err := idPath(id, "payment ID")
	if err != nil {
		return nil, err
	}
	return send[Payment](ctx, s.t, http.MethodGet, "/v1/payments/"+p, nil, nil, opts)
}

func (s *PaymentsService) List(ctx context.Context, params *PaymentListParams, opts ...RequestOption) (*PaymentList, error) {
	q := url.Values{}
	if params != nil {
		limitOffset(q, params.Limit, params.Offset)
		setIf(q, "status_filter", params.Status)
	}
	return send[PaymentList](ctx, s.t, http.MethodGet, "/v1/payments", q, nil, opts)
}

// ListAll walks every payment. params.Offset is ignored.
func (s *PaymentsService) ListAll(ctx context.Context, params *PaymentListParams, opts ...RequestOption) iter.Seq2[PaymentListItem, error] {
	base := PaymentListParams{}
	if params != nil {
		base = *params
	}
	return offsetPages(ctx, base.Limit, func(ctx context.Context, offset int) (page[PaymentListItem], error) {
		p := base
		p.Offset = offset
		list, err := s.List(ctx, &p, opts...)
		if err != nil {
			return page[PaymentListItem]{}, err
		}
		return page[PaymentListItem]{items: list.Items, total: list.Pagination.Total, hasMore: list.Pagination.HasMore}, nil
	})
}

// ---- Subscriptions ----

type SubscriptionsService struct{ t *transport }

func (s *SubscriptionsService) Get(ctx context.Context, id string, opts ...RequestOption) (*Subscription, error) {
	p, err := idPath(id, "subscription ID")
	if err != nil {
		return nil, err
	}
	return send[Subscription](ctx, s.t, http.MethodGet, "/v1/subscriptions/"+p, nil, nil, opts)
}

func (s *SubscriptionsService) List(ctx context.Context, params *SubscriptionListParams, opts ...RequestOption) (*SubscriptionList, error) {
	q := url.Values{}
	if params != nil {
		limitOffset(q, params.Limit, params.Offset)
		setIf(q, "customer_id", params.CustomerID)
		setIf(q, "status", params.Status)
	}
	return send[SubscriptionList](ctx, s.t, http.MethodGet, "/v1/subscriptions", q, nil, opts)
}

// ListAll walks every subscription. params.Offset is ignored.
func (s *SubscriptionsService) ListAll(ctx context.Context, params *SubscriptionListParams, opts ...RequestOption) iter.Seq2[Subscription, error] {
	base := SubscriptionListParams{}
	if params != nil {
		base = *params
	}
	return offsetPages(ctx, base.Limit, func(ctx context.Context, offset int) (page[Subscription], error) {
		p := base
		p.Offset = offset
		list, err := s.List(ctx, &p, opts...)
		if err != nil {
			return page[Subscription]{}, err
		}
		return page[Subscription]{items: list.Items, total: list.Pagination.Total, hasMore: list.Pagination.HasMore}, nil
	})
}

// Cancel cancels a subscription, immediately or at period end. params may be nil.
func (s *SubscriptionsService) Cancel(ctx context.Context, id string, params *CancelSubscriptionParams, opts ...RequestOption) (*Subscription, error) {
	p, err := idPath(id, "subscription ID")
	if err != nil {
		return nil, err
	}
	if params == nil {
		params = &CancelSubscriptionParams{}
	}
	return send[Subscription](ctx, s.t, http.MethodDelete, "/v1/subscriptions/"+p, nil, params, opts)
}

// ---- Customer portal ----

type CustomerPortalService struct{ t *transport }

// CreateSession returns a short-lived link to the hosted customer portal.
func (s *CustomerPortalService) CreateSession(ctx context.Context, customerID string, opts ...RequestOption) (*PortalSession, error) {
	p, err := idPath(customerID, "customer ID")
	if err != nil {
		return nil, err
	}
	return send[PortalSession](ctx, s.t, http.MethodPost, "/v1/customers/"+p+"/portal-sessions", nil, nil, opts)
}

// ---- Balances ----

type BalancesService struct{ t *transport }

func (s *BalancesService) Get(ctx context.Context, opts ...RequestOption) (*Balances, error) {
	return send[Balances](ctx, s.t, http.MethodGet, "/v1/balances", nil, nil, opts)
}

// ---- Banks ----

type BanksService struct{ t *transport }

// List returns the banks for a country code such as "NG". An empty country
// uses the API default.
func (s *BanksService) List(ctx context.Context, country string, opts ...RequestOption) (*BankList, error) {
	q := url.Values{}
	setIf(q, "country", country)
	return send[BankList](ctx, s.t, http.MethodGet, "/v1/reference/banks", q, nil, opts)
}

// ResolveAccount looks up the account name behind a bank account number.
func (s *BanksService) ResolveAccount(ctx context.Context, params *ResolveAccountParams, opts ...RequestOption) (*ResolvedAccount, error) {
	if params == nil {
		return nil, &ConfigError{Msg: "resolve account params are required"}
	}
	return send[ResolvedAccount](ctx, s.t, http.MethodPost, "/v1/misc/bank-accounts/resolve", nil, params, opts)
}

// ---- Payout destinations ----

type PayoutDestinationsService struct{ t *transport }

func (s *PayoutDestinationsService) Create(ctx context.Context, params *PayoutDestinationParams, opts ...RequestOption) (*PayoutDestination, error) {
	if params == nil {
		return nil, &ConfigError{Msg: "payout destination params are required"}
	}
	return send[PayoutDestination](ctx, s.t, http.MethodPost, "/v1/payouts/destinations", nil, params, opts)
}

func (s *PayoutDestinationsService) Get(ctx context.Context, id string, opts ...RequestOption) (*PayoutDestination, error) {
	p, err := idPath(id, "destination ID")
	if err != nil {
		return nil, err
	}
	return send[PayoutDestination](ctx, s.t, http.MethodGet, "/v1/payouts/destinations/"+p, nil, nil, opts)
}

func (s *PayoutDestinationsService) List(ctx context.Context, params *PayoutDestinationListParams, opts ...RequestOption) (*PayoutDestinationList, error) {
	q := url.Values{}
	if params != nil {
		limitOffset(q, params.Limit, params.Offset)
		setIf(q, "currency", params.Currency)
		setIf(q, "status", params.Status)
	}
	return send[PayoutDestinationList](ctx, s.t, http.MethodGet, "/v1/payouts/destinations", q, nil, opts)
}

// ListAll walks every payout destination. params.Offset is ignored.
func (s *PayoutDestinationsService) ListAll(ctx context.Context, params *PayoutDestinationListParams, opts ...RequestOption) iter.Seq2[PayoutDestination, error] {
	base := PayoutDestinationListParams{}
	if params != nil {
		base = *params
	}
	return offsetPages(ctx, base.Limit, func(ctx context.Context, offset int) (page[PayoutDestination], error) {
		p := base
		p.Offset = offset
		list, err := s.List(ctx, &p, opts...)
		if err != nil {
			return page[PayoutDestination]{}, err
		}
		total := list.Total
		return page[PayoutDestination]{items: list.Destinations, total: &total}, nil
	})
}

// ---- Payout quotes ----

type PayoutQuotesService struct{ t *transport }

// Create locks an exchange rate for a short time. Use the quote right away;
// an expired one fails with QUOTE_EXPIRED.
func (s *PayoutQuotesService) Create(ctx context.Context, params *PayoutQuoteParams, opts ...RequestOption) (*PayoutQuote, error) {
	if params == nil {
		return nil, &ConfigError{Msg: "payout quote params are required"}
	}
	return send[PayoutQuote](ctx, s.t, http.MethodPost, "/v1/payouts/quotes", nil, params, opts)
}

// ---- Payouts ----

type PayoutsService struct{ t *transport }

// Create sends a payout. Always pass WithIdempotencyKey: if the call times out,
// look the payout up before retrying, and retry only with the same key.
func (s *PayoutsService) Create(ctx context.Context, params *PayoutParams, opts ...RequestOption) (*Payout, error) {
	if params == nil || (params.Amount == "") == (params.QuoteID == "") {
		return nil, &ConfigError{Msg: "set exactly one of Amount or QuoteID"}
	}
	return send[Payout](ctx, s.t, http.MethodPost, "/v1/payouts", nil, params, opts)
}

func (s *PayoutsService) Get(ctx context.Context, id string, opts ...RequestOption) (*Payout, error) {
	p, err := idPath(id, "payout ID")
	if err != nil {
		return nil, err
	}
	return send[Payout](ctx, s.t, http.MethodGet, "/v1/payouts/"+p, nil, nil, opts)
}

func (s *PayoutsService) List(ctx context.Context, params *PayoutListParams, opts ...RequestOption) (*PayoutList, error) {
	q := url.Values{}
	if params != nil {
		limitOffset(q, params.Limit, params.Offset)
		setIf(q, "status_filter", params.Status)
	}
	return send[PayoutList](ctx, s.t, http.MethodGet, "/v1/payouts", q, nil, opts)
}

// ListAll walks every payout. params.Offset is ignored.
func (s *PayoutsService) ListAll(ctx context.Context, params *PayoutListParams, opts ...RequestOption) iter.Seq2[Payout, error] {
	base := PayoutListParams{}
	if params != nil {
		base = *params
	}
	return offsetPages(ctx, base.Limit, func(ctx context.Context, offset int) (page[Payout], error) {
		p := base
		p.Offset = offset
		list, err := s.List(ctx, &p, opts...)
		if err != nil {
			return page[Payout]{}, err
		}
		total := list.Total
		return page[Payout]{items: list.Items, total: &total}, nil
	})
}
