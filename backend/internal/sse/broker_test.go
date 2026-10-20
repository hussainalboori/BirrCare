package sse

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"birrcare/backend/internal/models"
)

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// flushRecorder is a goroutine-safe ResponseWriter + Flusher.
type flushRecorder struct {
	mu   sync.Mutex
	hdr  http.Header
	body strings.Builder
}

func newFlushRecorder() *flushRecorder       { return &flushRecorder{hdr: http.Header{}} }
func (f *flushRecorder) Header() http.Header { return f.hdr }
func (f *flushRecorder) WriteHeader(int)     {}
func (f *flushRecorder) Flush()              {}
func (f *flushRecorder) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.body.Write(p)
}
func (f *flushRecorder) String() string { f.mu.Lock(); defer f.mu.Unlock(); return f.body.String() }

func TestUnsubscribeOnContextDone(t *testing.T) {
	b := InitBroker()
	defer b.Close()

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/events/stream", nil).WithContext(ctx)
	rec := newFlushRecorder()
	done := make(chan struct{})
	go func() { b.ServeHTTP(rec, req); close(done) }()

	waitFor(t, "client registered", func() bool { return b.ClientCount() == 1 })

	b.Broadcast("MED_LOGGED", models.ActorInfo{ID: "cg_ali"}, map[string]string{"slot": "morning"})
	waitFor(t, "event delivered", func() bool { return strings.Contains(rec.String(), `"MED_LOGGED"`) })
	if !strings.Contains(rec.String(), "event: connected") {
		t.Fatalf("missing handshake: %q", rec.String())
	}

	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("ServeHTTP did not return after client disconnect")
	}
	waitFor(t, "client removed", func() bool { return b.ClientCount() == 0 })

	// Broadcasting after the client left must not panic (send on closed channel).
	b.Broadcast("VITAL_LOGGED", models.ActorInfo{}, nil)
	b.ClientCount()
}

func TestNoGoroutineLeak(t *testing.T) {
	baseline := runtime.NumGoroutine()

	b := InitBroker()
	const n = 50
	cancels := make([]context.CancelFunc, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		cancels[i] = cancel
		req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
		wg.Add(1)
		go func() { defer wg.Done(); b.ServeHTTP(newFlushRecorder(), req) }()
	}
	waitFor(t, "all clients registered", func() bool { return b.ClientCount() == n })

	for i := 0; i < 10; i++ {
		b.Broadcast("NOTE_ADDED", models.ActorInfo{}, i)
	}
	// Half disconnect themselves, the rest are dropped by broker shutdown.
	for i := 0; i < n/2; i++ {
		cancels[i]()
	}
	waitFor(t, "half removed", func() bool { return b.ClientCount() == n/2 })
	b.Close()
	wg.Wait()
	for _, c := range cancels {
		c()
	}

	waitFor(t, "goroutines back to baseline", func() bool {
		runtime.GC()
		return runtime.NumGoroutine() <= baseline
	})

	// Calls after Close must not block forever.
	b.Broadcast("X", models.ActorInfo{}, nil)
	b.Close()
	rec := httptest.NewRecorder()
	b.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 after close, got %d", rec.Code)
	}
}

func TestSlowClientDoesNotBlockBroadcaster(t *testing.T) {
	b := InitBroker()
	defer b.Close()

	slow := b.subscribe(4) // never read
	fast := b.subscribe(1000)
	received := make(chan int, 1)
	go func() {
		count := 0
		for range fast {
			count++
			if count == 200 {
				received <- count
			}
		}
	}()

	start := time.Now()
	for i := 0; i < 200; i++ {
		b.Broadcast("MED_LOGGED", models.ActorInfo{}, i)
	}
	if el := time.Since(start); el > 2*time.Second {
		t.Fatalf("broadcast blocked by slow client: %v", el)
	}

	select {
	case <-received:
	case <-time.After(5 * time.Second):
		t.Fatal("fast client did not receive all events")
	}

	// Slow client was evicted: its channel is closed after its buffer drains,
	// so its handler returns and the browser reconnects + refetches.
	drained := 0
	for range slow {
		drained++
	}
	if drained > 4 {
		t.Fatalf("slow client buffered more than its capacity: %d", drained)
	}
	if got := b.ClientCount(); got != 1 {
		t.Fatalf("expected only the fast client to remain, got %d", got)
	}
	b.unsubscribe(slow) // double unsubscribe after eviction must be a no-op
	b.unsubscribe(fast)
	waitFor(t, "fast removed", func() bool { return b.ClientCount() == 0 })
}

func TestHeartbeatOverHTTP(t *testing.T) {
	b := newBroker(50 * time.Millisecond)
	defer b.Close()
	srv := httptest.NewServer(b)
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type %q", ct)
	}
	sc := bufio.NewScanner(resp.Body)
	deadline := time.After(3 * time.Second)
	got := make(chan bool, 1)
	go func() {
		for sc.Scan() {
			if sc.Text() == ": ping" {
				got <- true
				return
			}
		}
	}()
	select {
	case <-got:
	case <-deadline:
		t.Fatal("no heartbeat received")
	}
}

// Issue #12: an event that created an alert carries the alert id as the SSE "id:".
func TestBroadcastAlertSetsEventID(t *testing.T) {
	b := InitBroker()
	defer b.Close()
	rec := newFlushRecorder()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go b.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/events/stream", nil).WithContext(ctx))
	waitFor(t, "client to connect", func() bool { return b.ClientCount() == 1 })

	b.BroadcastAlert("alert-123", "VITAL_LOGGED", models.ActorInfo{ID: "c1"}, map[string]string{"bp_status": "CRISIS"})
	b.Broadcast("NOTE_ADDED", models.ActorInfo{ID: "c1"}, map[string]string{})
	waitFor(t, "both events", func() bool { return strings.Count(rec.String(), "event: message") == 2 })

	out := rec.String()
	if !strings.Contains(out, "id: alert-123\nevent: message\ndata: {\"id\":\"alert-123\"") {
		t.Fatalf("alert event must carry id: alert-123, got:\n%s", out)
	}
	if strings.Count(out, "\nid: ") != 1 && !strings.HasPrefix(out, "id: ") {
		t.Fatalf("only alert events carry an SSE id, got:\n%s", out)
	}
}
