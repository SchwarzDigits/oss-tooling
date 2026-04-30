package inventory

import (
	"context"
	"sync"
)

// runPool processes items in parallel with at most n workers. The work
// function may return an error; errors are not aggregated by the pool —
// callers either record them on the item itself (preferred for fail-soft
// flows) or via a side channel they capture in the closure.
//
// runPool returns ctx.Err() if the context is canceled mid-flight; otherwise
// it always returns nil after waiting for in-flight workers to finish.
func runPool[T any](ctx context.Context, items []T, n int, work func(ctx context.Context, item *T) error) error {
	if n < 1 {
		n = 1
	}
	if len(items) == 0 {
		return nil
	}

	jobs := make(chan int, len(items))
	var wg sync.WaitGroup
	wg.Add(n)
	for w := 0; w < n; w++ {
		go func() {
			defer wg.Done()
			for i := range jobs {
				if ctx.Err() != nil {
					return
				}
				_ = work(ctx, &items[i])
			}
		}()
	}

	for i := range items {
		select {
		case jobs <- i:
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return ctx.Err()
		}
	}
	close(jobs)
	wg.Wait()
	return ctx.Err()
}
