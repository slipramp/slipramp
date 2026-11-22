package gate

import (
	"context"
	"log/slog"
	"testing"
	"testing/synctest"
	"time"
)

type windowRefreshRecorder struct {
	started time.Time
	calls   chan time.Time
}

func (r *windowRefreshRecorder) WindowCheckTime(context.Context) (time.Time, error) {
	r.started = time.Now()
	return r.started, nil
}

func (r *windowRefreshRecorder) RefreshServerWindows(_ context.Context, started time.Time) (time.Duration, error) {
	r.calls <- started
	return 15 * time.Second, nil
}

func TestWindowCoordinatorDeadlineAndPolicyChange(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		r := &windowRefreshRecorder{calls: make(chan time.Time, 4)}
		c := NewWindowCoordinator(r, slog.New(slog.DiscardHandler))
		go c.Run(ctx)
		synctest.Wait()
		time.Sleep(14 * time.Second)
		if len(r.calls) != 1 {
			t.Fatalf("early refresh: %d", len(r.calls))
		}
		time.Sleep(time.Second)
		synctest.Wait()
		if len(r.calls) != 2 {
			t.Fatalf("missed transition: %d", len(r.calls))
		}
		c.Notify()
		synctest.Wait()
		if len(r.calls) != 3 {
			t.Fatalf("missed policy change: %d", len(r.calls))
		}
		for range 3 {
			at := <-r.calls
			if at != r.started {
				t.Fatalf("startup cutoff moved: %v", at)
			}
		}
		cancel()
		synctest.Wait()
	})
}
