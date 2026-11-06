package memory

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/willunylabs/amsonia-core"
)

type tenantBoundary struct {
	name string
	run  func(context.Context, func() error) error
}

func tenantBoundaries(store *Store) []tenantBoundary {
	return []tenantBoundary{
		{name: "read", run: func(ctx context.Context, fn func() error) error {
			return store.ReadTenant(ctx, "tenant-a", func(amsonia.TenantReader) error { return fn() })
		}},
		{name: "mutate", run: func(ctx context.Context, fn func() error) error {
			return store.MutateTenant(ctx, "tenant-a", func(amsonia.TenantTx) error { return fn() })
		}},
		{name: "maintain", run: func(ctx context.Context, fn func() error) error {
			return store.MaintainTenant(ctx, "tenant-a", func(amsonia.MaintenanceTx) error { return fn() })
		}},
	}
}

func TestTenantBoundariesRejectCanceledContexts(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		for _, boundary := range tenantBoundaries(NewStore()) {
			t.Run(boundary.name+"/"+map[bool]string{false: "canceled", true: "expired"}[deadline], func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				if deadline {
					cancel()
					ctx, cancel = context.WithDeadline(t.Context(), time.Unix(0, 0))
				} else {
					cancel()
				}
				defer cancel()
				err := boundary.run(ctx, func() error {
					t.Error("canceled boundary invoked callback")
					return nil
				})
				if !errors.Is(err, ctx.Err()) {
					t.Errorf("error = %v; want %v", err, ctx.Err())
				}
			})
		}
	}
}

func TestTenantBoundariesCheckContextAfterCallback(t *testing.T) {
	callbackErr := errors.New("callback failed")
	for _, boundary := range tenantBoundaries(NewStore()) {
		for _, cause := range []error{nil, callbackErr} {
			t.Run(boundary.name+"/"+map[bool]string{false: "success", true: "failure"}[cause != nil], func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				err := boundary.run(ctx, func() error { cancel(); return cause })
				want := cause
				if want == nil {
					want = context.Canceled
				}
				if err != want {
					t.Errorf("error = %v; want original error %v", err, want)
				}
			})
		}
	}
}

// Signal after observing the initial live context. The test cancels it while
// the store's mutex is held, so the next boundary check must notice cancellation.
type observedContext struct {
	context.Context
	checked chan struct{}
	once    sync.Once
}

func (ctx *observedContext) Err() error {
	err := ctx.Context.Err()
	ctx.once.Do(func() { close(ctx.checked) })
	return err
}

func TestTenantBoundariesRecheckContextAfterLock(t *testing.T) {
	store := NewStore()
	for _, boundary := range tenantBoundaries(store) {
		t.Run(boundary.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			observed := &observedContext{Context: ctx, checked: make(chan struct{})}
			store.mu.Lock()
			done := make(chan error, 1)
			called := false
			go func() {
				done <- boundary.run(observed, func() error { called = true; return nil })
			}()
			select {
			case <-observed.checked:
			case <-time.After(5 * time.Second):
				store.mu.Unlock()
				<-done
				t.Fatal("boundary did not check context before acquiring lock")
			}
			cancel()
			store.mu.Unlock()
			if err := <-done; err != context.Canceled || called {
				t.Errorf("error = %v, callback called = %t; want cancellation without callback", err, called)
			}
		})
	}
}
