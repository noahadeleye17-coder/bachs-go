package bachs

import (
	"context"
	"iter"
)

// The API pages products by cursor and everything else by limit/offset, and
// not every list puts its items under the same field. ListAll methods hide
// that: they yield items one at a time and fetch the next page only when the
// caller reaches it.
//
//	for p, err := range client.Payments.ListAll(ctx, nil) {
//		if err != nil { return err }
//		fmt.Println(p.Status, p.Amount)
//	}
//
// Breaking out of the loop stops further requests. An error is yielded once,
// after which iteration ends.

type page[T any] struct {
	items []T
	// next is the cursor for cursor paging; total and hasMore are hints for offset paging.
	next    string
	total   *int
	hasMore *bool
}

func cursorPages[T any](ctx context.Context, fetch func(ctx context.Context, cursor string) (page[T], error)) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		cursor := ""
		for {
			p, err := fetch(ctx, cursor)
			if err != nil {
				var zero T
				yield(zero, err)
				return
			}
			for _, item := range p.items {
				if !yield(item, nil) {
					return
				}
			}
			// Guard against a server that hands back the same cursor forever.
			if p.next == "" || p.next == cursor {
				return
			}
			cursor = p.next
		}
	}
}

func offsetPages[T any](ctx context.Context, limit int, fetch func(ctx context.Context, offset int) (page[T], error)) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		offset := 0
		for {
			p, err := fetch(ctx, offset)
			if err != nil {
				var zero T
				yield(zero, err)
				return
			}
			for _, item := range p.items {
				if !yield(item, nil) {
					return
				}
			}
			offset += len(p.items)
			// Stop on an empty page (so a server that ignores offset cannot loop
			// forever), when the server says there is no more, on a short page,
			// or once the total is reached.
			done := len(p.items) == 0 ||
				(p.hasMore != nil && !*p.hasMore) ||
				(limit > 0 && len(p.items) < limit) ||
				(p.total != nil && offset >= *p.total)
			if done {
				return
			}
		}
	}
}
