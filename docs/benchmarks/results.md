# Benchmark results will be recorded here

## Environment Template

```
Date: YYYY-MM-DD
Machine: [CPU, RAM, Disk]
OS: [OS and version]
Docker: [version]
PostgreSQL: [version]
RelayDB: [commit]
```

## Results

| Scenario | Writers | Tx/s | Rows/Tx | Events/sec | p50 | p95 | p99 | Notes |
|----------|---------|------|---------|------------|-----|-----|-----|-------|
| A        | 1       | 10   | 10      | -          | -   | -   | -   |       |
| B        | 10      | 100  | 50      | -          | -   | -   | -   |       |
| C        | 50      | 500  | 100     | -          | -   | -   | -   |       |

## Profiles

- [CPU Profile](cpu.pprof)
- [Heap Profile](heap.pprof)
- [Goroutine Profile](goroutine.pprof)