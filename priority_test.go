package relay

import (
	"container/heap"
	"context"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jhonsferg/relay/testutil"
)

func TestPriorityConstants(t *testing.T) {
	tests := []struct {
		name     string
		priority Priority
		value    int
	}{
		{"PriorityLow", PriorityLow, 0},
		{"PriorityNormal", PriorityNormal, 50},
		{"PriorityHigh", PriorityHigh, 100},
		{"PriorityCritical", PriorityCritical, 200},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if int(tt.priority) != tt.value {
				t.Errorf("want %d, got %d", tt.value, int(tt.priority))
			}
		})
	}
}

func TestWithPriority(t *testing.T) {
	req := newRequest(http.MethodGet, "/test")

	// Default priority should be Normal
	if req.Priority() != PriorityNormal {
		t.Errorf("default priority: want %v, got %v", PriorityNormal, req.Priority())
	}

	// Set to High
	req = req.WithPriority(PriorityHigh)
	if req.Priority() != PriorityHigh {
		t.Errorf("after WithPriority(High): want %v, got %v", PriorityHigh, req.Priority())
	}

	// Set to Critical
	req = req.WithPriority(PriorityCritical)
	if req.Priority() != PriorityCritical {
		t.Errorf("after WithPriority(Critical): want %v, got %v", PriorityCritical, req.Priority())
	}

	// Verify chainability
	req2 := newRequest(http.MethodPost, "/api").
		WithPriority(PriorityHigh).
		WithHeader("X-Custom", "value")
	if req2.Priority() != PriorityHigh {
		t.Errorf("chained WithPriority: want %v, got %v", PriorityHigh, req2.Priority())
	}
}

func TestPriorityQueueOrdering(t *testing.T) {
	pq := newPriorityQueue()

	// Create test requests with different priorities.
	lowReq := newRequest(http.MethodGet, "/low").WithPriority(PriorityLow)
	normalReq := newRequest(http.MethodGet, "/normal").WithPriority(PriorityNormal)
	highReq := newRequest(http.MethodGet, "/high").WithPriority(PriorityHigh)
	criticalReq := newRequest(http.MethodGet, "/critical").WithPriority(PriorityCritical)

	// Pre-populate the heap (non-blocking) in random order.
	pq.enqueueDirect(normalReq, PriorityNormal)
	pq.enqueueDirect(lowReq, PriorityLow)
	pq.enqueueDirect(criticalReq, PriorityCritical)
	pq.enqueueDirect(highReq, PriorityHigh)

	// Dequeue in priority order (highest first).
	if req, p := pq.DequeueNext(); p != PriorityCritical || req != criticalReq {
		t.Errorf("first dequeue: want (criticalReq, %v), got (req, %v)", PriorityCritical, p)
	}

	if req, p := pq.DequeueNext(); p != PriorityHigh || req != highReq {
		t.Errorf("second dequeue: want (highReq, %v), got (req, %v)", PriorityHigh, p)
	}

	if req, p := pq.DequeueNext(); p != PriorityNormal || req != normalReq {
		t.Errorf("third dequeue: want (normalReq, %v), got (req, %v)", PriorityNormal, p)
	}

	if req, p := pq.DequeueNext(); p != PriorityLow || req != lowReq {
		t.Errorf("fourth dequeue: want (lowReq, %v), got (req, %v)", PriorityLow, p)
	}

	// Queue is empty.
	if req, _ := pq.DequeueNext(); req != nil {
		t.Error("dequeue from empty queue should return nil")
	}
}

func TestPriorityQueueFIFO(t *testing.T) {
	pq := newPriorityQueue()

	// Enqueue 3 requests with same priority.
	req1 := newRequest(http.MethodGet, "/1").WithPriority(PriorityNormal)
	req2 := newRequest(http.MethodGet, "/2").WithPriority(PriorityNormal)
	req3 := newRequest(http.MethodGet, "/3").WithPriority(PriorityNormal)

	pq.enqueueDirect(req1, PriorityNormal)
	pq.enqueueDirect(req2, PriorityNormal)
	pq.enqueueDirect(req3, PriorityNormal)

	// Should dequeue in FIFO order (lower sequence number first).
	if req, _ := pq.DequeueNext(); req != req1 {
		t.Error("first FIFO: want req1")
	}
	if req, _ := pq.DequeueNext(); req != req2 {
		t.Error("second FIFO: want req2")
	}
	if req, _ := pq.DequeueNext(); req != req3 {
		t.Error("third FIFO: want req3")
	}
}

func TestPriorityQueueContextCancellation(t *testing.T) {
	pq := newPriorityQueue()
	req := newRequest(http.MethodGet, "/test").WithPriority(PriorityHigh)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// EnqueueAndWait should return context error
	err := pq.EnqueueAndWait(ctx, req, PriorityHigh)
	if err == nil {
		t.Error("expected context cancellation error")
	}
	if err != context.Canceled {
		t.Errorf("want context.Canceled, got %v", err)
	}

	// Queue should still be empty
	if pq.Size() != 0 {
		t.Errorf("queue length: want 0, got %d", pq.Size())
	}
}

// TestPriorityQueue_RemoveFromMiddle_MaintainsHeapInvariant guards the O(n)
// -> O(log n) removeItem fix: removing an item that is NOT at the tail
// position (the only case the old linear-scan-then-heap.Remove code path
// and the new indexed one are guaranteed to agree on trivially) must still
// leave every remaining item's tracked heap index in sync with its actual
// slice position, and dequeue order must still be correct afterward.
func TestPriorityQueue_RemoveFromMiddle_MaintainsHeapInvariant(t *testing.T) {
	pq := newPriorityQueue()

	priorities := []Priority{10, 50, 30, 90, 20, 70, 40}
	items := make([]*priorityItem, len(priorities))
	pq.mu.Lock()
	for i, p := range priorities {
		item := &priorityItem{
			req:      newRequest(http.MethodGet, "/test"),
			priority: p,
			notify:   make(chan struct{}),
		}
		item.sequence = pq.sequence
		pq.sequence++
		heap.Push(pq, item)
		items[i] = item
	}
	pq.mu.Unlock()

	// Remove a middle-priority item (30) - neither the current max (90) nor
	// necessarily at the tail of the internal slice.
	target := items[2]
	pq.mu.Lock()
	removed := pq.removeItem(target)
	pq.mu.Unlock()
	if !removed {
		t.Fatal("expected removeItem to report success")
	}
	if target.index != -1 {
		t.Errorf("removed item's index = %d, want -1", target.index)
	}

	// Every remaining item's tracked index must match its actual slice
	// position - this is exactly what a Swap/Push/Pop bug would break.
	pq.mu.Lock()
	for i, it := range pq.items {
		if it.index != i {
			t.Errorf("item at slice position %d has stale index %d", i, it.index)
		}
	}
	pq.mu.Unlock()

	// Dequeue everything and confirm strictly descending priority order,
	// excluding the removed one (30).
	var gotOrder []Priority
	for pq.Size() > 0 {
		_, p := pq.DequeueNext()
		gotOrder = append(gotOrder, p)
	}
	wantOrder := []Priority{90, 70, 50, 40, 20, 10}
	if len(gotOrder) != len(wantOrder) {
		t.Fatalf("dequeued %d items, want %d: got %v", len(gotOrder), len(wantOrder), gotOrder)
	}
	for i := range wantOrder {
		if gotOrder[i] != wantOrder[i] {
			t.Errorf("dequeue order = %v, want %v", gotOrder, wantOrder)
			break
		}
	}

	// removeItem on an already-removed/dequeued item must report false, not
	// panic or corrupt the (now empty) heap.
	pq.mu.Lock()
	removedAgain := pq.removeItem(target)
	pq.mu.Unlock()
	if removedAgain {
		t.Error("removeItem on an already-removed item should return false")
	}
}

// TestPriorityQueue_ConcurrentMidQueueCancellations stresses removeItem
// specifically: many goroutines enqueue with a short, staggered timeout so
// a large fraction cancel while genuinely queued (not yet dequeued),
// forcing removeItem to remove items from arbitrary heap positions - not
// just the tail - under concurrent Push/Pop/Swap from other goroutines'
// enqueues and a slow dequeuer. Asserts no panic/deadlock and that every
// item is accounted for (either delivered or canceled, never both/neither).
func TestPriorityQueue_ConcurrentMidQueueCancellations(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	pq := newPriorityQueue()
	const total = 200

	var delivered, canceled atomic.Int64
	var wg sync.WaitGroup
	wg.Add(total)
	for i := 0; i < total; i++ {
		i := i
		go func() {
			defer wg.Done()
			// Stagger timeouts so cancellations land at many different
			// points relative to the queue's current shape, not all at once.
			timeout := time.Duration(1+i%7) * time.Millisecond
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			req := newRequest(http.MethodGet, fmt.Sprintf("/req%d", i))
			err := pq.EnqueueAndWait(ctx, req, Priority(i%4))
			if err != nil {
				canceled.Add(1)
			} else {
				delivered.Add(1)
			}
		}()
	}

	// A single slow, sporadic dequeuer so most items are still genuinely
	// queued (not yet dequeued) when their timeout fires.
	dequeuerDone := make(chan struct{})
	go func() {
		defer close(dequeuerDone)
		for delivered.Load()+canceled.Load() < total {
			if req, _ := pq.DequeueNext(); req == nil {
				time.Sleep(2 * time.Millisecond)
			}
			time.Sleep(500 * time.Microsecond)
		}
	}()

	wg.Wait()
	<-dequeuerDone

	if got := delivered.Load() + canceled.Load(); got != total {
		t.Errorf("delivered+canceled = %d, want %d (some request neither completed nor canceled)", got, total)
	}
	if pq.Size() != 0 {
		t.Errorf("queue should be empty after all goroutines finished, got size %d", pq.Size())
	}
}

// BenchmarkPriorityQueue_RemoveItem_LargeQueue measures removeItem's cost on
// a large, mostly-full queue removing from the middle - the O(n) linear
// scan this replaces would show clearly linear ns/op growth with queue
// size; the O(log n) indexed version should barely move.
func BenchmarkPriorityQueue_RemoveItem_LargeQueue(b *testing.B) {
	const size = 10_000
	pq := newPriorityQueue()
	items := make([]*priorityItem, size)
	pq.mu.Lock()
	for i := 0; i < size; i++ {
		item := &priorityItem{
			req:      newRequest(http.MethodGet, "/test"),
			priority: Priority(i % 100),
			notify:   make(chan struct{}),
		}
		item.sequence = pq.sequence
		pq.sequence++
		heap.Push(pq, item)
		items[i] = item
	}
	pq.mu.Unlock()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		// Re-add an item and immediately remove a different, arbitrary
		// (middle-ish) one each iteration to keep the queue near full size
		// throughout the benchmark.
		pq.mu.Lock()
		fresh := &priorityItem{req: items[0].req, priority: Priority(i % 100), notify: make(chan struct{})}
		fresh.sequence = pq.sequence
		pq.sequence++
		heap.Push(pq, fresh)
		target := items[i%size]
		pq.removeItem(target)
		items[i%size] = fresh
		pq.mu.Unlock()
	}
}

func TestPriorityQueueClose(t *testing.T) {
	pq := newPriorityQueue()
	pq.Close()

	req := newRequest(http.MethodGet, "/test")
	err := pq.EnqueueAndWait(context.Background(), req, PriorityNormal)
	if err == nil {
		t.Error("expected error after Close()")
	}
	if err != ErrClientClosed {
		t.Errorf("want ErrClientClosed, got %v", err)
	}
}

func TestClientWithoutPriorityQueue(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	client := New(
		WithBaseURL(srv.URL()),
		WithMaxConcurrentRequests(1),
		// NOT enabling priority queue
	)

	req := newRequest(http.MethodGet, "/test").WithPriority(PriorityHigh)
	resp, err := client.Execute(req)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("status: want 200, got %d", resp.StatusCode)
	}
}

func TestWithPriorityQueueOption(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()
	srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})

	client := New(
		WithBaseURL(srv.URL()),
		WithMaxConcurrentRequests(1),
		WithPriorityQueue(),
	)

	// Verify config was set
	if !client.config.EnablePriorityQueue {
		t.Error("EnablePriorityQueue not set via WithPriorityQueue()")
	}

	if client.priorityQueue == nil {
		t.Error("priorityQueue field not initialised")
	}

	req := newRequest(http.MethodGet, "/test").WithPriority(PriorityHigh)
	resp, err := client.Execute(req)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("status: want 200, got %d", resp.StatusCode)
	}
}

func TestClientPriorityQueueConcurrency(t *testing.T) {
	srv := testutil.NewMockServer()
	defer srv.Close()

	// Enqueue responses for 10 requests
	for i := 0; i < 10; i++ {
		srv.Enqueue(testutil.MockResponse{Status: http.StatusOK})
	}

	client := New(
		WithBaseURL(srv.URL()),
		WithMaxConcurrentRequests(2),
		WithPriorityQueue(),
	)

	var completedCount int32
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			priority := Priority((idx % 4) * 50) // Cycle through Low, Normal, High, High+
			req := newRequest(http.MethodGet, fmt.Sprintf("/req%d", idx)).WithPriority(priority)
			resp, err := client.Execute(req)
			if err == nil && resp.StatusCode == 200 {
				atomic.AddInt32(&completedCount, 1)
			}
		}(i)
	}

	wg.Wait()

	if completedCount != 10 {
		t.Errorf("completed %d requests, expected 10", completedCount)
	}
}

func TestPriorityQueueWithContextTimeout(t *testing.T) {
	pq := newPriorityQueue()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	req := newRequest(http.MethodGet, "/test")
	err := pq.EnqueueAndWait(ctx, req, PriorityHigh)

	// Should timeout since nothing will dequeue it
	if err == nil {
		t.Error("expected timeout error")
	}
}

func TestPriorityQueueStressTest(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	pq := newPriorityQueue()
	const numGoroutines = 50
	const requestsPerGoroutine = 20

	var wg sync.WaitGroup
	dequeueCount := int32(0)

	// Enqueuers
	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < requestsPerGoroutine; i++ {
				p := Priority(i % 4)
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				req := newRequest(http.MethodGet, fmt.Sprintf("/req%d_%d", g, i))
				_ = pq.EnqueueAndWait(ctx, req, p)
				cancel()
			}
		}()
	}

	// Dequeuers
	for d := 0; d < 5; d++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				req, _ := pq.DequeueNext()
				if req == nil {
					// Check if we're done
					if atomic.LoadInt32(&dequeueCount) >= int32(numGoroutines*requestsPerGoroutine) {
						return
					}
					time.Sleep(1 * time.Millisecond)
					continue
				}
				atomic.AddInt32(&dequeueCount, 1)
			}
		}()
	}

	wg.Wait()

	// Verify we got all items dequeued
	if dequeueCount != int32(numGoroutines*requestsPerGoroutine) {
		t.Errorf("expected %d items dequeued, got %d", numGoroutines*requestsPerGoroutine, dequeueCount)
	}
}

func BenchmarkPriorityQueueEnqueue(b *testing.B) {
	pq := newPriorityQueue()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		req := newRequest(http.MethodGet, fmt.Sprintf("/req%d", i))
		pq.enqueueDirect(req, PriorityNormal)
	}
}

func BenchmarkPriorityQueueDequeue(b *testing.B) {
	pq := newPriorityQueue()

	// Pre-populate with requests using non-blocking enqueue.
	for i := 0; i < b.N; i++ {
		req := newRequest(http.MethodGet, fmt.Sprintf("/req%d", i))
		pq.enqueueDirect(req, PriorityNormal)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		pq.DequeueNext()
	}
}
