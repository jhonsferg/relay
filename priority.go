package relay

import (
	"container/heap"
	"context"
	"sync"
)

// Priority represents the urgency level of a request. Higher values indicate
// higher priority and are dequeued first when the bulkhead is at capacity.
// Within the same priority level, requests are dequeued in FIFO order.
type Priority int

const (
	// PriorityLow is for background or non-critical requests.
	PriorityLow Priority = 0
	// PriorityNormal is the default priority for typical requests.
	PriorityNormal Priority = 50
	// PriorityHigh is for important requests that should execute sooner.
	PriorityHigh Priority = 100
	// PriorityCritical is for time-sensitive requests (health checks, auth, etc.).
	PriorityCritical Priority = 200
)

// priorityItem wraps a Request with its enqueue sequence for FIFO
// ordering within the same priority level.
type priorityItem struct {
	req      *Request
	priority Priority
	sequence uint64
	notify   chan struct{} // closed when this item is dequeued
}

// priorityQueue implements container/heap.Interface for a max-heap where
// higher priority values are dequeued first. Within the same priority,
// lower sequence numbers (earlier arrivals) are dequeued first (FIFO).
type priorityQueue struct {
	items    []*priorityItem
	sequence uint64
	mu       sync.Mutex
	closed   bool
}

// newPriorityQueue creates a new empty priority queue.
func newPriorityQueue() *priorityQueue {
	return &priorityQueue{
		items: make([]*priorityItem, 0),
	}
}

// enqueueDirect adds a request directly to the heap without blocking.
// The item's notify channel is closed immediately so any hypothetical
// waiter would unblock right away. Use only in tests that want to
// pre-populate the queue and verify ordering without a running client.
func (pq *priorityQueue) enqueueDirect(req *Request, priority Priority) {
	notify := make(chan struct{})
	item := &priorityItem{
		req:      req,
		priority: priority,
		notify:   notify,
	}
	pq.mu.Lock()
	item.sequence = pq.sequence
	pq.sequence++
	heap.Push(pq, item)
	pq.mu.Unlock()
}

// it is dequeued, respecting the context deadline. Returns an error if
// the context is cancelled or the queue is closed.
func (pq *priorityQueue) EnqueueAndWait(ctx context.Context, req *Request, priority Priority) error {
	notify := make(chan struct{})
	item := &priorityItem{
		req:      req,
		priority: priority,
		notify:   notify,
	}

	pq.mu.Lock()
	if pq.closed {
		pq.mu.Unlock()
		return ErrClientClosed
	}

	item.sequence = pq.sequence
	pq.sequence++
	heap.Push(pq, item)
	pq.mu.Unlock()

	// Wait for either dequeue notification or context cancellation
	select {
	case <-notify:
		return nil
	case <-ctx.Done():
		// Remove from queue if still there
		pq.mu.Lock()
		pq.removeItem(item)
		pq.mu.Unlock()
		return ctx.Err()
	}
}

// removeItem removes a specific item from the queue. Must be called under lock.
func (pq *priorityQueue) removeItem(target *priorityItem) {
	for i, item := range pq.items {
		if item == target {
			pq.items = append(pq.items[:i], pq.items[i+1:]...)
			if i < len(pq.items) {
				heap.Fix(pq, i)
			}
			return
		}
	}
}

// DequeueNext dequeues and returns the highest-priority request from the queue.
// Notifies the waiting request so it can proceed.
func (pq *priorityQueue) DequeueNext() (*Request, Priority) {
	pq.mu.Lock()
	defer pq.mu.Unlock()

	if len(pq.items) == 0 {
		return nil, PriorityNormal
	}

	item := heap.Pop(pq).(*priorityItem)
	close(item.notify)
	return item.req, item.priority
}

// Size returns the number of items currently in the queue. Safe to call
// from outside; acquires the lock.
func (pq *priorityQueue) Size() int {
	pq.mu.Lock()
	defer pq.mu.Unlock()
	return len(pq.items)
}

// Close marks the queue as closed, causing new EnqueueAndWait calls to fail.
func (pq *priorityQueue) Close() {
	pq.mu.Lock()
	defer pq.mu.Unlock()
	pq.closed = true
}

// Implement container/heap.Interface
// NOTE: these methods are called by container/heap while pq.mu is already
// held by the caller (EnqueueAndWait / DequeueNext). They must NOT acquire
// the mutex to avoid deadlocks.

// Len satisfies heap.Interface. Must be called with pq.mu held.
func (pq *priorityQueue) Len() int {
	return len(pq.items)
}

// Less compares heap items for max-heap ordering.
// Not safe to call without holding pq.mu.
func (pq *priorityQueue) Less(i, j int) bool {
	// Max-heap: higher priority value comes first
	if pq.items[i].priority != pq.items[j].priority {
		return pq.items[i].priority > pq.items[j].priority
	}
	// FIFO within same priority: lower sequence comes first
	return pq.items[i].sequence < pq.items[j].sequence
}

func (pq *priorityQueue) Swap(i, j int) {
	pq.items[i], pq.items[j] = pq.items[j], pq.items[i]
}

func (pq *priorityQueue) Push(x interface{}) {
	pq.items = append(pq.items, x.(*priorityItem))
}

func (pq *priorityQueue) Pop() interface{} {
	old := pq.items
	n := len(old)
	x := old[n-1]
	pq.items = old[0 : n-1]
	return x
}
