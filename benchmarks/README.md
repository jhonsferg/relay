# Relay HTTP Client - Benchmarks

High-performance benchmarks for the relay HTTP client library. These tests measure throughput, memory efficiency, concurrency behavior, and scalability across different workload patterns.

## Running Benchmarks

### All benchmarks
```bash
go test -bench=. -benchmem -count=5 ./benchmarks/...
```

### Specific benchmark category
```bash
go test -bench=BenchmarkThroughput -benchmem -count=5 ./benchmarks/
go test -bench=BenchmarkMemory -benchmem -count=5 ./benchmarks/
go test -bench=BenchmarkConcurrency -benchmem -count=5 ./benchmarks/
go test -bench=BenchmarkScalability -benchmem -count=5 ./benchmarks/
go test -bench=BenchmarkConnectionPooling -benchmem -count=5 ./benchmarks/
```

### Compare with baseline
```bash
go test -bench=. -benchmem -count=5 ./benchmarks/ > new_results.txt
benchstat old_results.txt new_results.txt
```

## Benchmark Categories

### Throughput
- `BenchmarkThroughput_SmallPayloads`: Measures ops/sec with tiny responses (< 100 bytes)
- `BenchmarkThroughput_MediumPayloads`: Measures ops/sec with 50 KB responses
- `BenchmarkThroughput_Sequential`: Baseline single-threaded performance
- `BenchmarkThroughput_HighConcurrency`: Parallel throughput at GOMAXPROCS scale
- `BenchmarkThroughput_POSTRequests`: Write operation throughput

### Memory
- `BenchmarkMemory_LargePayload1MB`: Tests 1 MB response handling
- `BenchmarkMemory_LargePayload10MB`: Tests 10 MB response handling
- `BenchmarkMemory_HighGCPressure`: Forces GC between iterations to measure heap growth

### Concurrency
- `BenchmarkConcurrency_HighParallelism`: Measures mutex contention under maximum goroutine load
- `BenchmarkConcurrency_Sequential`: Single-threaded baseline for comparison

### Scalability
- `BenchmarkScalability_1KBPayload`: Performance with minimal payloads
- `BenchmarkScalability_100KBPayload`: Performance scaling to 100 KB
- `BenchmarkScalability_1MBPayload`: Performance with 1 MB payloads

### Connection Pooling
- `BenchmarkConnectionPooling_SingleConnection`: Serial request baseline (pool size 1)
- `BenchmarkConnectionPooling_OptimalPoolSize`: Balanced pool configuration (50-100 connections)

## Metrics

- **ns/op**: Nanoseconds per operation (lower is better)
- **allocs/op**: Number of allocations per operation (lower is better, target: < 4)
- **B/op**: Bytes allocated per operation (lower is better)
- **ops/sec**: Calculated from throughput benchmarks (higher is better)

## Performance Targets

After optimization phases 1-6:
- Memory reduction: > 12% vs Standard HTTP
- Latency improvement: > 8% in concurrent scenarios
- Throughput: > 26,000 ops/sec for small payloads
- Allocs/op: < 4 (critical path optimized)

## Notes

- Benchmarks use local httptest.NewServer() for network overhead simulation
- Real-world performance may vary based on network latency and bandwidth
- GC pressure benchmarks include explicit `runtime.GC()` calls
- All benchmarks report allocation counts for memory-sensitive workloads
