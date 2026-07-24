package scheduler

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/conductor-ai/conductor/core/ports"
)

// fakeRunner records each RunTriggered call and only knows the workflows it was
// seeded with, so job validation and firing are both observable.
type fakeRunner struct {
	mu    sync.Mutex
	known map[string]bool
	calls []string // workflow names, in fire order
	block chan struct{}
}

func (f *fakeRunner) Has(name string) bool { return f.known[name] }

func (f *fakeRunner) RunTriggered(_ context.Context, name string, _ map[string]string, trigger string) (ports.WorkflowRun, error) {
	if f.block != nil {
		<-f.block // hold the run open to exercise overlap-skip
	}
	f.mu.Lock()
	f.calls = append(f.calls, name+":"+trigger)
	f.mu.Unlock()
	return ports.WorkflowRun{ID: "wfr", Status: ports.RunSuccess, CreatedUnix: 1}, nil
}

func (f *fakeRunner) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func testTime(t *testing.T, s string) time.Time {
	t.Helper()
	tm, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return tm
}

// New must reject a bad cron expression and an unknown workflow, and accept a
// valid job.
func TestNew_Validation(t *testing.T) {
	r := &fakeRunner{known: map[string]bool{"wf": true}}

	if _, err := New([]JobSpec{{Name: "j", Workflow: "wf", Cron: "not-cron"}}, r, quietLog()); err == nil {
		t.Fatal("expected error for bad cron")
	}
	if _, err := New([]JobSpec{{Name: "j", Workflow: "missing", Cron: "* * * * *"}}, r, quietLog()); err == nil {
		t.Fatal("expected error for unknown workflow")
	}
	if _, err := New([]JobSpec{{Name: "j", Workflow: "wf", Cron: "* * * * *"}}, r, quietLog()); err != nil {
		t.Fatalf("valid job rejected: %v", err)
	}
}

// A job fires once its scheduled minute arrives — and the first tick only seeds
// next-fire (no backfill), tagging the run as schedule-triggered.
func TestTick_FiresWhenDue(t *testing.T) {
	r := &fakeRunner{known: map[string]bool{"wf": true}}
	s, err := New([]JobSpec{{Name: "j", Workflow: "wf", Cron: "*/5 * * * *"}}, r, quietLog())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// First tick only seeds next; nothing fires yet.
	s.tick(ctx, testTime(t, "2026-07-23T09:02:00Z"))
	if n := r.callCount(); n != 0 {
		t.Fatalf("first tick should not fire, got %d calls", n)
	}
	// A tick before the next 5-minute boundary (09:05) still does not fire.
	s.tick(ctx, testTime(t, "2026-07-23T09:04:00Z"))
	if n := r.callCount(); n != 0 {
		t.Fatalf("pre-boundary tick fired early, got %d calls", n)
	}
	// At/after the boundary it fires, tagged schedule.
	s.tick(ctx, testTime(t, "2026-07-23T09:05:00Z"))
	waitFor(t, func() bool { return r.callCount() == 1 })
	if r.calls[0] != "wf:"+ports.TriggerSchedule {
		t.Fatalf("wrong call: %q", r.calls[0])
	}
}

// A fire is skipped while the previous run of the same job is still in progress.
func TestTick_SkipsOverlap(t *testing.T) {
	r := &fakeRunner{known: map[string]bool{"wf": true}, block: make(chan struct{})}
	s, err := New([]JobSpec{{Name: "j", Workflow: "wf", Cron: "* * * * *"}}, r, quietLog())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	s.tick(ctx, testTime(t, "2026-07-23T09:00:00Z")) // seed
	s.tick(ctx, testTime(t, "2026-07-23T09:01:00Z")) // fires, then blocks in RunTriggered
	s.tick(ctx, testTime(t, "2026-07-23T09:02:00Z")) // should skip (still running)

	// Only one run should be in flight; release it and confirm exactly one call.
	close(r.block)
	waitFor(t, func() bool { return r.callCount() == 1 })
	// Give any erroneous second fire a chance to land before asserting.
	time.Sleep(20 * time.Millisecond)
	if n := r.callCount(); n != 1 {
		t.Fatalf("overlap not skipped: got %d calls", n)
	}
}

// waitFor polls cond for up to a second so goroutine-launched fires are observed
// without a fixed sleep.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("condition not met within deadline")
}
