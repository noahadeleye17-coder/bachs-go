// Package bachs is a community Go client for the Bachs payments API (https://docs.bachs.io).
//
// It is not an official Bachs product. It follows the behaviour of the official
// Node.js SDK: reads are retried on transient failures, writes are never retried
// automatically, and every write can carry an idempotency key.
//
//	client, err := bachs.New(os.Getenv("BACHS_API_KEY"))
//	session, err := client.CheckoutSessions.Create(ctx, &bachs.CheckoutSessionParams{...},
//		bachs.WithIdempotencyKey(orderID))
//
// Webhook verification lives in the webhook subpackage, so a webhook-only
// service does not need an API key.
package bachs

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Version is the SDK version sent in the User-Agent header.
const Version = "0.1.0"

// Environment is the Bachs environment a client talks to.
type Environment string

const (
	Sandbox    Environment = "sandbox"
	Production Environment = "production"
)

var hosts = map[Environment]string{
	Sandbox:    "https://sandbox-api.bachs.io",
	Production: "https://api.bachs.io",
}

const (
	defaultTimeout    = 30 * time.Second
	defaultMaxRetries = 2
)

// Client is safe for concurrent use. Create one and reuse it.
type Client struct {
	Environment Environment

	Products           *ProductsService
	CheckoutSessions   *CheckoutSessionsService
	Payments           *PaymentsService
	Subscriptions      *SubscriptionsService
	CustomerPortal     *CustomerPortalService
	Balances           *BalancesService
	Banks              *BanksService
	PayoutDestinations *PayoutDestinationsService
	PayoutQuotes       *PayoutQuotesService
	Payouts            *PayoutsService

	t *transport
}

// Option configures a Client.
type Option func(*config) error

type config struct {
	environment Environment
	baseURL     string
	httpClient  *http.Client
	timeout     time.Duration
	maxRetries  int
	logger      *slog.Logger
}

// WithEnvironment pins the environment. If the key's prefix says otherwise,
// New returns an error instead of sending anything.
func WithEnvironment(env Environment) Option {
	return func(c *config) error {
		if env != Sandbox && env != Production {
			return fmt.Errorf("environment must be %q or %q", Sandbox, Production)
		}
		c.environment = env
		return nil
	}
}

// WithBaseURL overrides the API host, for a proxy or a local test server.
// It must be HTTPS, or plain HTTP on a loopback address.
func WithBaseURL(raw string) Option {
	return func(c *config) error { c.baseURL = raw; return nil }
}

// WithHTTPClient sets the HTTP client used for requests. The SDK never follows
// redirects, whatever the client's own policy is.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *config) error {
		if hc == nil {
			return errors.New("http client cannot be nil")
		}
		c.httpClient = hc
		return nil
	}
}

// WithTimeout sets the default budget for a whole operation, including
// retries and waits. The default is 30 seconds. A shorter context deadline wins.
func WithTimeout(d time.Duration) Option {
	return func(c *config) error {
		if d <= 0 {
			return errors.New("timeout must be positive")
		}
		c.timeout = d
		return nil
	}
}

// WithMaxRetries sets how many extra attempts a read gets. The default is 2.
// Writes are never retried.
func WithMaxRetries(n int) Option {
	return func(c *config) error {
		if n < 0 {
			return errors.New("max retries cannot be negative")
		}
		c.maxRetries = n
		return nil
	}
}

// WithLogger sends request logs to l. Without it the SDK logs nothing.
// The API key is never logged.
func WithLogger(l *slog.Logger) Option {
	return func(c *config) error { c.logger = l; return nil }
}

// KeyEnvironment reports the environment a key belongs to from its prefix.
// ok is false for a prefix the SDK does not recognise.
func KeyEnvironment(apiKey string) (env Environment, ok bool) {
	switch {
	case strings.HasPrefix(apiKey, "sk_sandbox_"):
		return Sandbox, true
	case strings.HasPrefix(apiKey, "sk_live_"):
		return Production, true
	}
	return "", false
}

// New creates a client. The environment is taken from the key prefix
// (sk_sandbox_ or sk_live_) unless WithEnvironment is given.
func New(apiKey string, opts ...Option) (*Client, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, &ConfigError{Msg: "an API key is required"}
	}
	if apiKey != strings.TrimSpace(apiKey) || strings.ContainsAny(apiKey, "\r\n") {
		return nil, &ConfigError{Msg: "the API key cannot contain surrounding whitespace or line breaks"}
	}
	cfg := config{timeout: defaultTimeout, maxRetries: defaultMaxRetries}
	for _, opt := range opts {
		if err := opt(&cfg); err != nil {
			return nil, &ConfigError{Msg: err.Error()}
		}
	}

	detected, known := KeyEnvironment(apiKey)
	env := cfg.environment
	switch {
	case !known && env == "":
		return nil, &ConfigError{Msg: "the API key prefix is not recognised (expected sk_sandbox_ or sk_live_); use WithEnvironment"}
	case known && env != "" && env != detected:
		// A sandbox key in production config (or the reverse) is never intended.
		return nil, &ConfigError{Msg: fmt.Sprintf("environment is %q but the API key is a %s key", env, detected)}
	case known:
		env = detected
	}

	base, err := parseBaseURL(cfg.baseURL, env)
	if err != nil {
		return nil, err
	}

	hc := http.DefaultClient
	if cfg.httpClient != nil {
		hc = cfg.httpClient
	}
	// Copy so we can refuse redirects without changing the caller's client.
	noRedirect := *hc
	noRedirect.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	t := &transport{
		apiKey:     apiKey,
		baseURL:    base,
		http:       &noRedirect,
		timeout:    cfg.timeout,
		maxRetries: cfg.maxRetries,
		log:        cfg.logger,
	}
	c := &Client{Environment: env, t: t}
	c.Products = &ProductsService{t}
	c.CheckoutSessions = &CheckoutSessionsService{t}
	c.Payments = &PaymentsService{t}
	c.Subscriptions = &SubscriptionsService{t}
	c.CustomerPortal = &CustomerPortalService{t}
	c.Balances = &BalancesService{t}
	c.Banks = &BanksService{t}
	c.PayoutDestinations = &PayoutDestinationsService{t}
	c.PayoutQuotes = &PayoutQuotesService{t}
	c.Payouts = &PayoutsService{t}
	return c, nil
}

func parseBaseURL(raw string, env Environment) (*url.URL, error) {
	if raw == "" {
		raw = hosts[env]
	}
	u, err := url.Parse(raw)
	if err != nil || !u.IsAbs() {
		return nil, &ConfigError{Msg: "base URL must be an absolute URL", Err: err}
	}
	host := u.Hostname()
	loopback := host == "localhost" || host == "127.0.0.1" || host == "::1"
	if (u.Scheme != "https" && !(u.Scheme == "http" && loopback)) || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, &ConfigError{Msg: "base URL must use HTTPS (or HTTP on loopback), with no credentials, query or fragment"}
	}
	u.Path = strings.TrimSuffix(u.Path, "/")
	return u, nil
}
