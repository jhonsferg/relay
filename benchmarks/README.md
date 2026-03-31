# Relay HTTP Client - Benchmarks

Comprehensive performance benchmarks for the relay HTTP client library, organized by feature category and use case.

## Directory Structure

The benchmarks are organized into focused subdirectories by purpose:

### `/common`
Core benchmarks measuring fundamental Execute() performance with minimal features enabled.

- `execute_test.go`: Basic throughput, latency, retry, cache, batch, and async execution benchmarks

**Run:**
```bash
go test -bench=BenchmarkExecute ./benchmarks/common/ -benchmem -count=3
```

### `/bigdata`
Benchmarks for large-payload processing and high-volume data transfer scenarios.

- `large_payloads_test.go`: 50k-record JSON arrays, multi-MB responses, GC pressure, memory efficiency

**Scenarios:**
- Batch API responses with thousands of records
- File downloads and streaming
- Memory consumption under sustained high-volume throughput

**Run:**
```bash
go test -bench=BenchmarkBatch ./benchmarks/bigdata/ -benchmem -count=3
```

### `/memory`
Benchmarks focused on memory allocation patterns, buffer reuse, and garbage collection behavior.

- `allocation_test.go`: Allocation overhead by payload size, GC pressure, buffer pooling efficiency, header parsing

**Scenarios:**
- Small responses (< 1 KB) - baseline allocation
- Medium responses (10-100 KB) - typical paginated APIs
- Large responses (>= 1 MB) - file transfers
- Concurrent allocation patterns
- Forced GC cycles
- Header parsing overhead

**Run:**
```bash
go test -bench=BenchmarkMemory ./benchmarks/memory/ -benchmem -count=3
```

### `/concurrency`
Benchmarks measuring concurrent request handling, goroutine contention, and parallel scaling.

- `contention_test.go`: Parallel requests, high contention, rate limiting, multiple clients, burst traffic, circuit breaker impact

**Scenarios:**
- Sequential baseline (single goroutine)
- Parallel requests (RunParallel)
- High contention (thousands of goroutines)
- Burst traffic with recovery
- Rate-limited load
- Multiple isolated clients

**Run:**
```bash
go test -bench=BenchmarkConcurrency ./benchmarks/concurrency/ -benchmem -count=3
```

### `/connection_pooling`
Benchmarks for HTTP connection pool behavior, reuse efficiency, and pool size impact.

- `pool_strategies_test.go`: Default pool, minimal pool, optimal pool, aggressive pool, connection reuse, multi-host, idle timeout, keep-alive, pool exhaustion

**Scenarios:**
- Pool size comparison (1, 50, 1000 connections)
- Connection reuse efficiency
- Multi-host load distribution
- Keep-Alive disabled performance
- Pool exhaustion under burst load

**Run:**
```bash
go test -bench=BenchmarkConnectionPooling ./benchmarks/connection_pooling/ -benchmem -count=3
```

## Running All Benchmarks

Run all benchmarks across all categories:

```bash
go test -bench=Benchmark ./benchmarks/... -benchmem -count=3
```

Run benchmarks with CPU profiling:

```bash
go test -bench=Benchmark ./benchmarks/... -benchmem -cpuprofile=cpu.prof
go tool pprof cpu.prof
```

## Metrics

- **ns/op**: Nanoseconds per operation (lower is better)
- **allocs/op**: Number of allocations per operation (lower is better, target: < 4)
- **B/op**: Bytes allocated per operation (lower is better)

## Performance Targets

After optimization phases 1-6:

| Metric | Target | Status |
|--------|--------|--------|
| Memory reduction vs Standard HTTP | > 12% | ✓ Achieved |
| Latency improvement (concurrent) | > 8% | ✓ Achieved |
| Small payload throughput | > 26,000 ops/sec | ✓ Achieved |
| Allocs per operation | < 4 | ✓ Optimized |
| GC pressure | Minimal | ✓ Pooling enabled |

## Benchmarking Best Practices

1. **Isolate tests**: Run each benchmark category separately for clean cache behavior
2. **Multiple runs**: Use `-count=3` or higher for statistical significance
3. **Compare baselines**: Capture baseline results before making changes
4. **Use benchstat**: Compare results with `benchstat` tool:
   ```bash
   benchstat old.txt new.txt
   ```

## CI/CD Integration

Example GitHub Actions workflow:

```yaml
- name: Run benchmarks
  run: |
    go test -bench=Benchmark ./benchmarks/... -benchmem -count=5 > bench_new.txt
    
- name: Compare with baseline
  if: github.event_name == 'pull_request'
  run: |
    go run golang.org/x/perf/cmd/benchstat@latest bench_baseline.txt bench_new.txt
```

## Regression Detection

Monitor for performance regressions by tracking key metrics:

```bash
benchstat old.txt new.txt | grep -E "allocs/op|B/op|ns/op" | grep -E "\+[5-9]%|\+[1-9][0-9]%"
```

Regressions >= 10% in these metrics warrant investigation.

## Notes

- All benchmarks use local mock servers to simulate network overhead
- Real-world performance depends on network latency and bandwidth
- Allocation counts are crucial for high-throughput scenarios
- GC pressure benchmarks include explicit `runtime.GC()` calls
- Circuit breaker and rate limiting impacts are separately measurable

