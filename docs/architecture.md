# Architecture

## Overview
The project is a production-grade wget-like CLI implemented in Go, optimized for Linux-based Docker deployment with cross-platform compatibility.

Core goals:
- correctness and reliability first
- bounded concurrency (default `10`, max `50`)
- robust retry/resume behavior
- safe mirroring within same registrable domain by default

## Module Map
- `cmd/wget`
  - application entrypoint and lifecycle
- `internal/cli`
  - pflag-based GNU-style parsing and validation
- `internal/httpclient`
  - shared HTTP client/transport (timeouts, pooling, proxy env support)
- `internal/downloader`
  - streaming download engine with retries, resume, and transfer metadata
- `internal/fs`
  - path safety and write constraints
- `internal/progress`
  - TTY progress bar + non-TTY periodic progress lines
- `internal/logger`
  - `log/slog`-based human/JSON logging (`--debug`, `--trace`)
- `internal/ratelimiter`
  - global token-bucket rate limiting (binary units)
- `internal/concurrency`
  - bounded worker pool and aggregate failure handling
- `internal/mirror`
  - recursive crawl, robots handling, filters, link conversion
- `internal/parser`
  - HTML parsing via `golang.org/x/net/html` with link extraction from `a`, `link`, `img`, `script`, and CSS `url(...)` in mirrored HTML
- `internal/errcode`
  - machine-readable error taxonomy and classification

## Data Flow
1. CLI flags are parsed into runtime config.
2. Mode dispatch chooses single download, input-file batch, or mirror flow.
3. Concurrency manager schedules jobs and collects outcomes.
4. Downloader streams bytes to `.part`, applies retry/backoff, and finalizes on success.
5. Mirror engine discovers links, enforces scope/policies, and enqueues resources.
6. Progress and logging provide operational visibility across all modes.

## Error Flow
- Typed errors are emitted close to failure source, then wrapped upward.
- Aggregated failures include machine-readable codes.
- Retryable status codes: `408`, `429`, `5xx`.
- Retry exhaustion includes per-attempt metadata:
  - status code
  - backoff used
  - per-attempt bytes
  - cumulative partial bytes

## Mirror Policy
- Scope: same registrable domain (`eTLD+1`)
- Allowed scheme change: `http <-> https` within domain
- Default max depth: `5`
- Robots:
  - default: fetch/parse failures are non-fatal
  - strict mode: `--strict-robots` fails on robots fetch/parse errors
- Safety caps:
  - max pages
  - max total bytes
- Reject filter behavior:
  - supports extension/glob forms (`gif`, `.gif`, `*.gif`)

## Concurrency and Performance
- Worker pool constrained to max `50`.
- Memory optimizations focus first on mirror path.
- Allocation pressure reduced for default concurrency path.
- Buffer pooling used in downloader and mirror copy paths.
