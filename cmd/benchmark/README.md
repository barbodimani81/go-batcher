# Cargo Performance Benchmark

This tool tests the throughput and performance of the cargo batcher under various load conditions.

## Usage

```bash
go run cmd/benchmark/main.go [flags]
```

### Flags

- `-rps` - Requests per second to simulate (default: 1000)
- `-duration` - How long to run the test (default: 10s)
- `-batch-size` - Batch size for cargo (default: 100)
- `-timeout` - Flush timeout for cargo (default: 50ms)

## Examples

### Test 1,000 RPS
```bash
go run cmd/benchmark/main.go -rps=1000 -duration=10s
```

### Test 5,000 RPS
```bash
go run cmd/benchmark/main.go -rps=5000 -duration=5s -batch-size=500 -timeout=20ms
```

### Test 10,000 RPS
```bash
go run cmd/benchmark/main.go -rps=10000 -duration=5s -batch-size=1000 -timeout=10ms
```

### Stress Test (50,000 RPS)
```bash
go run cmd/benchmark/main.go -rps=50000 -duration=3s -batch-size=5000 -timeout=5ms
```

## Metrics Explained

- **Added/s** - Items added to cargo per second
- **Processed/s** - Items processed by handler per second
- **Batches** - Total number of batch flushes
- **Errors** - Failed Add() operations
- **Actual RPS** - Measured requests per second
- **Avg Batch Size** - Average items per batch
- **Processing Rate** - Items processed per second

## Performance Tips

1. **Higher RPS** → Increase batch size and decrease timeout
2. **Lower latency** → Decrease batch size and timeout
3. **Memory efficiency** → Balance batch size with your item size
4. **Handler speed** → Faster handlers = higher throughput

## Benchmark Tests

For automated benchmarking, use Go's built-in benchmark tool:

```bash
# Run all benchmarks
go test -bench=. -benchmem

# Run specific benchmark
go test -bench=BenchmarkCargo1000RPS -benchtime=10s

# With CPU profiling
go test -bench=. -cpuprofile=cpu.prof

# With memory profiling
go test -bench=. -memprofile=mem.prof
```
