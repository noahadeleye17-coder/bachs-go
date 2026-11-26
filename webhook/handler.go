package webhook

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// MaxBodyBytes caps a delivery body read by Handler.
const MaxBodyBytes = 1 << 20

// Claim is the result of Store.Begin.
type Claim int

const (
	// Claimed means this delivery should be processed now.
	Claimed Claim = iota
	// Done means the event was already processed; acknowledge and skip.
	Done
	// InProgress means another delivery of the same event is being processed.
	InProgress
)

// Store records which events have been handled, so duplicates are dropped.
//
// Bachs delivers at least once, retries for about 80 minutes, and lets you
// replay events by hand, so the same event ID can arrive more than once and
// even at the same time. In production back this with your database, for
// example a table with a unique event_id column, so it survives restarts and
// works across instances.
type Store interface {
	// Begin claims an event ID. It must be atomic across concurrent callers.
	Begin(ctx context.Context, eventID string) (Claim, error)
	// Finish records the outcome. On failure the claim is released so a
	// retry can process the event again.
	Finish(ctx context.Context, eventID string, succeeded bool) error
}

// HandlerFunc processes one verified event. Return an error to make Bachs retry.
// Bachs waits 10 seconds for a 2xx, so do slow work in the background.
type HandlerFunc func(ctx context.Context, e *Event) error

// HandlerOptions configures Handler.
type HandlerOptions struct {
	// Store drops duplicate deliveries. Nil disables de-duplication.
	Store Store
	// Verify options, such as WithTolerance or WithAdditionalSecrets.
	Verify []Option
	// Logger receives one line per rejected or failed delivery. Optional.
	Logger *slog.Logger
}

// Handler returns an http.Handler for a Bachs webhook endpoint. Status codes are
// chosen for Bachs's retry rules (it retries 5xx, 408 and 429, not other 4xx):
//
//   - bad signature or body: 400, not retried
//   - duplicate of a finished event: 200
//   - duplicate still being processed elsewhere: 503, retried later
//   - fn or the store fails: 500, retried
//   - success: 200
func Handler(secret string, fn HandlerFunc, opts *HandlerOptions) http.Handler {
	if opts == nil {
		opts = &HandlerOptions{}
	}
	logf := func(msg string, args ...any) {
		if opts.Logger != nil {
			opts.Logger.Warn(msg, args...)
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBodyBytes))
		if err != nil {
			var tooBig *http.MaxBytesError
			if errors.As(err, &tooBig) {
				http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, "could not read body", http.StatusBadRequest)
			return
		}
		ev, err := ConstructEvent(body, r.Header, secret, opts.Verify...)
		if err != nil {
			logf("bachs webhook rejected", "error", err)
			http.Error(w, "invalid webhook", http.StatusBadRequest)
			return
		}

		ctx := r.Context()
		if opts.Store != nil {
			claim, err := opts.Store.Begin(ctx, ev.ID)
			if err != nil {
				logf("bachs webhook store failed", "event", ev.ID, "error", err)
				http.Error(w, "store unavailable", http.StatusInternalServerError)
				return
			}
			switch claim {
			case Done:
				w.WriteHeader(http.StatusOK)
				return
			case InProgress:
				http.Error(w, "already processing", http.StatusServiceUnavailable)
				return
			}
		}

		handleErr := fn(ctx, ev)
		if opts.Store != nil {
			// Record the outcome even if the request context was cancelled.
			finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			if err := opts.Store.Finish(finishCtx, ev.ID, handleErr == nil); err != nil {
				logf("bachs webhook store failed", "event", ev.ID, "error", err)
				if handleErr == nil {
					// Processed but not recorded; a retry may run it again,
					// which the caller's idempotent handler must tolerate.
					handleErr = err
				}
			}
			cancel()
		}
		if handleErr != nil {
			logf("bachs webhook handler failed", "event", ev.ID, "type", ev.Type, "error", handleErr)
			http.Error(w, "handler failed", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
}

// MemoryStore is an in-process Store, fine for one instance and for tests.
// Finished IDs are forgotten after TTL (default 7 days).
type MemoryStore struct {
	TTL time.Duration

	mu    sync.Mutex
	state map[string]memEntry
	now   func() time.Time
}

type memEntry struct {
	done bool
	at   time.Time
}

func (m *MemoryStore) Begin(_ context.Context, id string) (Claim, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.clock()
	if m.state == nil {
		m.state = map[string]memEntry{}
	}
	m.prune(now)
	if e, ok := m.state[id]; ok {
		if e.done {
			return Done, nil
		}
		return InProgress, nil
	}
	m.state[id] = memEntry{at: now}
	return Claimed, nil
}

func (m *MemoryStore) Finish(_ context.Context, id string, succeeded bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state == nil {
		m.state = map[string]memEntry{}
	}
	if succeeded {
		m.state[id] = memEntry{done: true, at: m.clock()}
	} else {
		delete(m.state, id)
	}
	return nil
}

func (m *MemoryStore) clock() time.Time {
	if m.now != nil {
		return m.now()
	}
	return time.Now()
}

func (m *MemoryStore) prune(now time.Time) {
	ttl := m.TTL
	if ttl <= 0 {
		ttl = 7 * 24 * time.Hour
	}
	for id, e := range m.state {
		// In-progress claims older than a minute belong to a crashed handler.
		limit := ttl
		if !e.done {
			limit = time.Minute
		}
		if now.Sub(e.at) > limit {
			delete(m.state, id)
		}
	}
}
