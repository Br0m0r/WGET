# Testing

## Test Types
- Unit tests: module-level behavior under `internal/*`
- Integration tests: cross-module flows under `tests/`
- Stress/concurrency tests: gated by build tag `stress`
- Profiling benchmarks: downloader, concurrency manager, and mirror crawler

## Required Policies
- Coverage threshold:
  - total coverage for `internal/*` must be `>= 80%`
- Race checks:
  - mandatory `go test -race ./...`
- Stress/concurrency:
  - run with `-tags=stress`

## Commands
- Default test suite:
```bash
go test ./...
```
- Stress-tagged suite:
```bash
go test -tags=stress ./...
```
- Coverage (internal packages only):
```bash
go test ./internal/... -covermode=atomic -coverprofile coverage_internal.out
go tool cover -func=coverage_internal.out
```
- Race tests:
```bash
CGO_ENABLED=1 go test -race ./...
```

## Scripted Execution
- Linux/macOS shell:
```bash
./scripts/test.sh
./scripts/test.sh --stress
```
- PowerShell:
```powershell
.\scripts\test.ps1
.\scripts\test.ps1 -Stress
```

## Stress Tag Notes
Stress tests are intentionally excluded from default runs and must be explicitly enabled:
- file: `tests/stress_concurrency_test.go`
- build tag: `stress`

## Docker/CI Troubleshooting
If full checks fail in local/CI:
- Coverage stage fails:
  - verify Go coverage tooling is available in your environment
  - rerun in the Docker base image used for deployment
- Race stage fails (`gcc` missing):
  - install `gcc` (or compatible C compiler)
  - set `CGO_ENABLED=1`

Recommended CI order:
1. `go test ./...`
2. coverage check for `internal/*`
3. `CGO_ENABLED=1 go test -race ./...`
4. `go test -tags=stress ./...`

## Profiling Guide
Use profiling when you need to investigate CPU, memory, allocation pressure, or throughput behavior.

Profiled paths:
- downloader
- concurrency manager
- mirror crawler

Workloads:
- single large file
- 10 concurrent downloads
- 50 concurrent downloads
- mirror crawl on synthetic test site

Store all profiling artifacts under:
- `scripts/profiles/`

## Profiling Commands
Downloader profile:
```bash
go test ./internal/downloader -run '^$' -bench '^BenchmarkProfileDownloaderSingleLargeFile$' -benchmem -count=1 -cpuprofile scripts/profiles/downloader_cpu.pprof -memprofile scripts/profiles/downloader_mem.pprof
```

Concurrency profiles:
```bash
go test ./internal/concurrency -run '^$' -bench '^BenchmarkManager_ConcurrencyEfficiency/workers_10$' -benchmem -count=1 -cpuprofile scripts/profiles/concurrency_10_cpu.pprof -memprofile scripts/profiles/concurrency_10_mem.pprof
go test ./internal/concurrency -run '^$' -bench '^BenchmarkManager_ConcurrencyEfficiency/workers_50$' -benchmem -count=1 -cpuprofile scripts/profiles/concurrency_50_cpu.pprof -memprofile scripts/profiles/concurrency_50_mem.pprof
```

Mirror profile:
```bash
go test ./internal/mirror -run '^$' -bench '^BenchmarkProfileMirrorSyntheticSite$' -benchmem -count=1 -cpuprofile scripts/profiles/mirror_cpu.pprof -memprofile scripts/profiles/mirror_mem.pprof
```

Top summaries:
```bash
go tool pprof -top scripts/profiles/downloader_cpu.pprof
go tool pprof -top scripts/profiles/downloader_mem.pprof
go tool pprof -top scripts/profiles/concurrency_10_cpu.pprof
go tool pprof -top scripts/profiles/concurrency_10_mem.pprof
go tool pprof -top scripts/profiles/concurrency_50_cpu.pprof
go tool pprof -top scripts/profiles/concurrency_50_mem.pprof
go tool pprof -top scripts/profiles/mirror_cpu.pprof
go tool pprof -top scripts/profiles/mirror_mem.pprof
```

## Profiling Artifacts
Typical generated files:
- benchmark output (`*_bench.txt`) if captured
- CPU profile (`*_cpu.pprof`)
- memory profile (`*_mem.pprof`)
- optional summary files (`*_top.txt`)

## Profiling Troubleshooting
- Missing profile files:
  - ensure output paths are under `scripts/profiles/`
  - verify write permissions in the workspace/container
- Unexpected variance:
  - use stable CPU/memory conditions
  - avoid heavy background load while profiling
  - compare multiple runs rather than a single sample
