package bachs

import "encoding/json"

// Money amounts are decimal strings at the currency's precision ("29.00"),
// exactly as the API sends them. The SDK never converts them to float64.

// Ptr returns a pointer to v. Handy for optional request fields.
func Ptr[T any](v T) *T { return &v }

// Pagination is the cursor/offset block on list responses.
type Pagination struct {
	NextCursor *string `json:"next_cursor,omitempty"`
	PrevCursor *string `json:"prev_cursor,omitempty"`
	HasMore    *bool   `json:"has_more,omitempty"`
	Limit      int     `json:"limit,omitempty"`
	Offset     int     `json:"offset,omitempty"`
	Returned   int     `json:"returned,omitempty"`
	Total      *int    `json:"total,omitempty"`
}

// BillingCycle is a recurring cadence, e.g. every 1 month.
type BillingCycle struct {
	Interval  string `json:"interval,omitempty"` // day, week, month, year
	Frequency int    `json:"frequency,omitempty"`
}

// ---- Products ----

// CurrencyOption is an extra currency price on a product.
type CurrencyOption struct {
	Currency      string  `json:"currency"`
	Amount        *string `json:"amount,omitempty"`
	PresetAmount  *string `json:"preset_amount,omitempty"`
	MinimumAmount *string `json:"minimum_amount,omitempty"`
	MaximumAmount *string `json:"maximum_amount,omitempty"`
}

// PriceParams sets a product price. PriceType is "fixed" (default), "free" or "custom".
type PriceParams struct {
	Currency        string           `json:"currency"`
	PriceType       string           `json:"price_type,omitempty"`
	Amount          *string          `json:"amount,omitempty"`
	PresetAmount    *string          `json:"preset_amount,omitempty"`
	MinimumAmount   *string          `json:"minimum_amount,omitempty"`
	MaximumAmount   *string          `json:"maximum_amount,omitempty"`
	CurrencyOptions []CurrencyOption `json:"currency_options,omitempty"`
}

// ProductParams creates a product. Set BillingCycle to make it recurring.
type ProductParams struct {
	Name         string         `json:"name"`
	Description  *string        `json:"description,omitempty"`
	Price        PriceParams    `json:"price"`
	Metadata     map[string]any `json:"metadata,omitempty"`
	BillingCycle *BillingCycle  `json:"billing_cycle,omitempty"`
}

type Price struct {
	Currency        string           `json:"currency"`
	PriceType       string           `json:"price_type"`
	Amount          string           `json:"amount"`
	PresetAmount    *string          `json:"preset_amount"`
	MinimumAmount   *string          `json:"minimum_amount"`
	MaximumAmount   *string          `json:"maximum_amount"`
	CurrencyOptions []CurrencyOption `json:"currency_options"`
}

type Product struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	Description    *string        `json:"description"`
	Price          Price          `json:"price"`
	OrganizationID string         `json:"organization_id"`
	Status         string         `json:"status"`
	Metadata       map[string]any `json:"metadata"`
	BillingCycle   *BillingCycle  `json:"billing_cycle"`
	TotalPayments  int            `json:"total_payments"`
	TotalAmount    string         `json:"total_amount"`
	CreatedAt      string         `json:"created_at"`
	UpdatedAt      string         `json:"updated_at"`
	ArchivedAt     *string        `json:"archived_at"`
}

type ProductListParams struct {
	Limit           int
	Cursor          string
	IncludeArchived bool
}

type ProductList struct {
	Items      []Product  `json:"items"`
	Pagination Pagination `json:"pagination"`
}

// ---- Checkout sessions ----

// CustomerInput identifies the buyer: either CustomerID, or Email and Name.
type CustomerInput struct {
	CustomerID  string  `json:"customer_id,omitempty"`
	Email       string  `json:"email,omitempty"`
	Name        string  `json:"name,omitempty"`
	PhoneNumber *string `json:"phone_number,omitempty"`
}

type CartPriceOverride struct {
	PriceType     string  `json:"price_type,omitempty"`
	Amount        *string `json:"amount,omitempty"`
	PresetAmount  *string `json:"preset_amount,omitempty"`
	MinimumAmount *string `json:"minimum_amount,omitempty"`
	MaximumAmount *string `json:"maximum_amount,omitempty"`
}

type CartItem struct {
	ProductID string             `json:"product_id"`
	Quantity  int                `json:"quantity,omitempty"`
	Pricing   *CartPriceOverride `json:"pricing,omitempty"`
}

// CheckoutPricing charges a raw amount without a product.
type CheckoutPricing struct {
	Currency        string            `json:"currency"`
	Amount          *string           `json:"amount,omitempty"`
	PriceType       string            `json:"price_type,omitempty"`
	PresetAmount    *string           `json:"preset_amount,omitempty"`
	MinimumAmount   *string           `json:"minimum_amount,omitempty"`
	MaximumAmount   *string           `json:"maximum_amount,omitempty"`
	CurrencyOptions map[string]string `json:"currency_options,omitempty"`
}

// Payment method corridors for CheckoutSessionParams.PaymentMethodTypes.
const (
	CorridorUSDCard         = "USD_CARD"
	CorridorNGNCard         = "NGN_CARD"
	CorridorNGNBankTransfer = "NGN_BANK_TRANSFER"
	CorridorMomoGHS         = "MOMO_GHS"
	CorridorMomoKES         = "MOMO_KES"
	CorridorCrypto          = "CRYPTO"
)

// CheckoutSessionParams creates a hosted checkout. Set exactly one of
// ProductCart or Pricing.
type CheckoutSessionParams struct {
	ProductCart        []CartItem       `json:"product_cart,omitempty"`
	Pricing            *CheckoutPricing `json:"pricing,omitempty"`
	Customer           *CustomerInput   `json:"customer,omitempty"`
	CustomerCreation   string           `json:"customer_creation,omitempty"` // always | if_required
	BillingCurrency    *string          `json:"billing_currency,omitempty"`
	PaymentMethodTypes []string         `json:"payment_method_types,omitempty"`
	SuccessURL         string           `json:"success_url,omitempty"`
	CancelURL          *string          `json:"cancel_url,omitempty"`
	Reference          *string          `json:"reference,omitempty"`
	Metadata           map[string]any   `json:"metadata,omitempty"`
	ExpiresInMinutes   int              `json:"expires_in_minutes,omitempty"` // 1 to 1440, default 60
}

type CheckoutSessionCreated struct {
	CheckoutID  string  `json:"checkout_id"`
	CheckoutURL string  `json:"checkout_url"`
	Status      string  `json:"status"`
	ExpiresAt   string  `json:"expires_at"`
	CreatedAt   string  `json:"created_at"`
	Reference   *string `json:"reference"`
}

type CheckoutIdentity struct {
	ID    *string `json:"id"`
	Email *string `json:"email"`
	Name  *string `json:"name"`
}

type CheckoutSession struct {
	CheckoutID      string            `json:"checkout_id"`
	Status          string            `json:"status"`
	Amount          string            `json:"amount"`
	Currency        string            `json:"currency"`
	PaymentStatus   *string           `json:"payment_status"`
	Reference       *string           `json:"reference"`
	Charge          *Payment          `json:"charge"`
	PaymentMethod   *string           `json:"payment_method"`
	Customer        *CheckoutIdentity `json:"customer"`
	CustomerDetails *CheckoutIdentity `json:"customer_details"`
	SuccessURL      *string           `json:"success_url"`
	CancelURL       *string           `json:"cancel_url"`
	BillingCurrency *string           `json:"billing_currency"`
	SessionMode     *string           `json:"session_mode"`
	Metadata        map[string]any    `json:"metadata"`
	CreatedAt       string            `json:"created_at"`
	ExpiresAt       *string           `json:"expires_at"`
	CompletedAt     *string           `json:"completed_at"`
	UpdatedAt       string            `json:"updated_at"`
}

// ---- Payments ----

type Payment struct {
	PaymentID       string         `json:"payment_id"`
	Status          string         `json:"status"`
	Amount          string         `json:"amount"`
	Currency        string         `json:"currency"`
	Reference       *string        `json:"reference"`
	CheckoutID      *string        `json:"checkout_id"`
	SubscriptionID  *string        `json:"subscription_id"`
	BillingReason   string         `json:"billing_reason"`
	FeeUSD          *string        `json:"fee_usd"`
	AmountPaid      *string        `json:"amount_paid"`
	AmountRemaining *string        `json:"amount_remaining"`
	PaymentMethod   *string        `json:"payment_method"`
	IsRefundable    *bool          `json:"is_refundable"`
	Meta            map[string]any `json:"meta"`
	CreatedAt       string         `json:"created_at"`
	UpdatedAt       string         `json:"updated_at"`
	CompletedAt     *string        `json:"completed_at"`
}

// PaymentListItem is the shorter shape returned by the payments list.
type PaymentListItem struct {
	ID                 *string `json:"id"`
	Status             string  `json:"status"`
	Amount             string  `json:"amount"`
	Currency           string  `json:"currency"`
	CustomerName       string  `json:"customer_name"`
	CustomerEmail      string  `json:"customer_email"`
	Reference          *string `json:"reference"`
	AmountPaid         *string `json:"amount_paid"`
	AmountRemaining    *string `json:"amount_remaining"`
	SettlementAmount   *string `json:"settlement_amount"`
	SettlementCurrency *string `json:"settlement_currency"`
	Fee                *string `json:"fee"`
	VAT                *string `json:"vat"`
	TransactionDate    *string `json:"transaction_date"`
	CompletedAt        *string `json:"completed_at"`
}

type PaymentListParams struct {
	Limit  int
	Offset int
	Status string // e.g. succeeded, failed, refunded, underpaid
}

type PaymentList struct {
	Items      []PaymentListItem `json:"items"`
	Pagination Pagination        `json:"pagination"`
}

// ---- Subscriptions ----

type SubscriptionCustomer struct {
	CustomerID  string  `json:"customer_id"`
	Email       *string `json:"email"`
	Name        *string `json:"name"`
	PhoneNumber *string `json:"phone_number"`
}

type SubscriptionItem struct {
	ID                 string  `json:"id"`
	Status             string  `json:"status"`
	Quantity           int     `json:"quantity"`
	Recurring          bool    `json:"recurring"`
	PriceType          string  `json:"price_type"`
	UnitAmount         string  `json:"unit_amount"`
	Currency           string  `json:"currency"`
	PreviouslyBilledAt *string `json:"previously_billed_at"`
	NextBilledAt       *string `json:"next_billed_at"`
}

// Subscription statuses.
const (
	SubscriptionTrialing = "trialing"
	SubscriptionActive   = "active"
	SubscriptionPastDue  = "past_due"
	SubscriptionUnpaid   = "unpaid"
	SubscriptionCanceled = "canceled"
)

type Subscription struct {
	ID                 string                `json:"id"`
	Status             string                `json:"status"`
	Customer           *SubscriptionCustomer `json:"customer"`
	PaymentMethodID    *string               `json:"payment_method_id"`
	CollectionMethod   string                `json:"collection_method"`
	Currency           string                `json:"currency"`
	Amount             string                `json:"amount"`
	BillingCycle       *BillingCycle         `json:"billing_cycle"`
	Quantity           int                   `json:"quantity"`
	CurrentPeriodStart string                `json:"current_period_start"`
	CurrentPeriodEnd   string                `json:"current_period_end"`
	PreviouslyBilledAt *string               `json:"previously_billed_at"`
	NextBilledAt       *string               `json:"next_billed_at"`
	TrialEnd           *string               `json:"trial_end"`
	CancelAtPeriodEnd  bool                  `json:"cancel_at_period_end"`
	CanceledAt         *string               `json:"canceled_at"`
	CreatedAt          string                `json:"created_at"`
	Items              []SubscriptionItem    `json:"items"`
	Metadata           map[string]any        `json:"metadata"`
}

type SubscriptionListParams struct {
	Limit      int
	Offset     int
	CustomerID string
	Status     string
}

type SubscriptionList struct {
	Items      []Subscription `json:"items"`
	Pagination Pagination     `json:"pagination"`
}

type CancelSubscriptionParams struct {
	CancelAtPeriodEnd *bool   `json:"cancel_at_period_end,omitempty"`
	Reason            *string `json:"reason,omitempty"`
}

// ---- Customer portal ----

type PortalSession struct {
	ID  string `json:"id"`
	URL string `json:"url"`
}

// ---- Balances ----

type Balance struct {
	Currency         string `json:"currency"`
	AvailableBalance string `json:"available_balance"`
	PendingBalance   string `json:"pending_balance"`
	HeldForDisputes  string `json:"held_for_disputes"`
}

type Balances struct {
	AccountID               string            `json:"account_id"`
	Balances                []Balance         `json:"balances"`
	TotalBalanceUSD         string            `json:"total_balance_usd"`
	PendingSettlementsByDay []json.RawMessage `json:"pending_settlements_by_day"`
}

// ---- Banks ----

type Bank struct {
	Name string `json:"name"`
	Code string `json:"code"`
}

type BankList struct {
	Country string `json:"country"`
	Banks   []Bank `json:"banks"`
}

type ResolveAccountParams struct {
	AccountNumber string  `json:"account_number"`
	BankCode      string  `json:"bank_code"`
	Country       *string `json:"country,omitempty"`
}

type ResolvedAccount struct {
	Resolved      bool    `json:"resolved"`
	AccountName   *string `json:"account_name"`
	AccountNumber *string `json:"account_number"`
	Message       *string `json:"message"`
}

// ---- Payouts ----

type PayoutDestinationParams struct {
	Currency        string         `json:"currency"`
	Name            *string        `json:"name,omitempty"`
	Label           *string        `json:"label,omitempty"`
	Type            *string        `json:"type,omitempty"`
	DestinationType *string        `json:"destination_type,omitempty"`
	AccountNumber   *string        `json:"account_number,omitempty"`
	BankCode        *string        `json:"bank_code,omitempty"`
	AccountName     *string        `json:"account_name,omitempty"`
	BankName        *string        `json:"bank_name,omitempty"`
	PhoneNumber     *string        `json:"phone_number,omitempty"`
	MobileProvider  *string        `json:"mobile_provider,omitempty"`
	WalletAddress   *string        `json:"wallet_address,omitempty"`
	Network         *string        `json:"network,omitempty"`
	Metadata        map[string]any `json:"metadata,omitempty"`
}

type PayoutDestination struct {
	ID             string  `json:"id"`
	Name           string  `json:"name"`
	Type           string  `json:"type"`
	Currency       string  `json:"currency"`
	Status         string  `json:"status"` // pending_review, approved, rejected
	IsUsable       bool    `json:"is_usable"`
	IsDefault      bool    `json:"is_default"`
	StatusReason   *string `json:"status_reason"`
	AccountNumber  *string `json:"account_number"`
	AccountName    *string `json:"account_name"`
	BankCode       *string `json:"bank_code"`
	BankName       *string `json:"bank_name"`
	PhoneNumber    *string `json:"phone_number"`
	MobileProvider *string `json:"mobile_provider"`
	WalletAddress  *string `json:"wallet_address"`
	Network        *string `json:"network"`
	ReviewedAt     *string `json:"reviewed_at"`
	CreatedAt      *string `json:"created_at"`
	UpdatedAt      *string `json:"updated_at"`
}

type PayoutDestinationListParams struct {
	Currency string
	Status   string
	Limit    int
	Offset   int
}

// PayoutDestinationList uses "destinations" rather than "items"; the SDK keeps
// the API's shape here and hides the difference in ListAll.
type PayoutDestinationList struct {
	Destinations []PayoutDestination `json:"destinations"`
	Total        int                 `json:"total"`
	Limit        int                 `json:"limit"`
	Offset       int                 `json:"offset"`
}

type PayoutQuoteParams struct {
	FromCurrency string  `json:"from_currency"`
	ToCurrency   string  `json:"to_currency"`
	Amount       string  `json:"amount"`
	PayoutMethod *string `json:"payout_method,omitempty"`
}

type PayoutQuote struct {
	QuoteID      string `json:"quote_id"`
	FromCurrency string `json:"from_currency"`
	ToCurrency   string `json:"to_currency"`
	FromAmount   string `json:"from_amount"`
	ToAmount     string `json:"to_amount"`
	ExchangeRate string `json:"exchange_rate"`
	ExpiresAt    string `json:"expires_at"`
}

// PayoutParams sends money to a saved destination. Set exactly one of Amount
// or QuoteID.
type PayoutParams struct {
	Destination string         `json:"destination"`
	Amount      string         `json:"amount,omitempty"`
	QuoteID     string         `json:"quote_id,omitempty"`
	Reference   *string        `json:"reference,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

type Payout struct {
	ID             string  `json:"id"`
	Status         string  `json:"status"`
	Amount         string  `json:"amount"`
	Currency       string  `json:"currency"`
	SourceCurrency *string `json:"source_currency"`
	Fee            *string `json:"fee"`
	TotalDebited   *string `json:"total_debited"`
	Destination    *string `json:"destination"`
	Reference      *string `json:"reference"`
	FailureReason  *string `json:"failure_reason"`
	CreatedAt      *string `json:"created_at"`
	CompletedAt    *string `json:"completed_at"`
}

type PayoutListParams struct {
	Limit  int
	Offset int
	Status string
}

type PayoutList struct {
	Items []Payout `json:"items"`
	Total int      `json:"total"`
}
