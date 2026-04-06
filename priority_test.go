package relay

import (
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
