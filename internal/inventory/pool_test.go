package inventory

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type item struct {
	id     int
	worked bool
	failed bool
}

func TestRunPool_AllProcessed(t *testing.T) {
	t.Parallel()

	items := make([]item, 100)
	for i := range items {
		items[i].id = i
	}

	var processed atomic.Int32
	err := runPool(context.Background(), items, 10, func(_ context.Context, it *item) error {
		processed.Add(1)
		it.worked = true
		// Mix in some "errors" — should not abort the pool.
		if it.id%7 == 0 {
			it.failed = true
			return errors.New("boom")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("runPool: %v", err)
	}
	if processed.Load() != 100 {
		t.Fatalf("expected 100 processed, got %d", processed.Load())
	}
	for i, it := range items {
		if !it.worked {
			t.Errorf("item %d not worked", i)
		}
	}
}

func TestRunPool_CancelStops(t *testing.T) {
	t.Parallel()

	items := make([]item, 200)
	for i := range items {
		items[i].id = i
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	err := runPool(ctx, items, 4, func(ctx context.Context, it *item) error {
		select {
		case <-time.After(50 * time.Millisecond):
			it.worked = true
		case <-ctx.Done():
			return ctx.Err()
		}
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	worked := 0
	for _, it := range items {
		if it.worked {
			worked++
		}
	}
	if worked >= len(items) {
		t.Fatalf("expected partial work, got %d/%d", worked, len(items))
	}
}

func TestRunPool_EmptyInput(t *testing.T) {
	t.Parallel()
	var items []item
	err := runPool(context.Background(), items, 5, func(context.Context, *item) error {
		t.Fatal("work should not be called for empty input")
		return nil
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}
