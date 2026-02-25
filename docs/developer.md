# Developer Guide

## Project Layout
- `cmd/wget`: CLI entrypoint
- `internal/*`: implementation modules
- `tests/`: integration and stress tests
- `scripts/`: test/profiling helpers
- `scripts/profiles/`: profiling artifacts
- `phase-execution.md`: phase-by-phase execution record

## Toolchain
- Go version in module: `1.24.0`
- Primary dependencies:
  - `github.com/spf13/pflag`
  - `golang.org/x/net/html`

## Development Workflow
1. Implement within relevant `internal/*` module.
2. Add or update unit/integration tests.
3. Run:
   - `go test ./...`
   - `go test -tags=stress ./...` (when stress paths are touched)
4. Keep docs in `docs/` aligned with behavior.

## Error Contracts
- Use machine-readable codes via `internal/errcode`.
- Preserve wrapped causes (`%w`) for root-cause introspection.
- For retrying flows, include attempt-level metadata in terminal errors.
- For aggregate results, include:
  - URL/job context
  - error code
  - wrapped error

## Logging
- Base logger: `log/slog`
- Default output: human-readable
- Optional JSON output
- `--debug`: DEBUG level
- `--trace`: TRACE behavior, implies DEBUG
- Stack traces on ERROR only in debug/trace mode

## Mirror Behavior Notes
- default scope: same registrable domain
- robots failures: non-fatal by default
- strict robots mode available with `--strict-robots`
- links rewritten only for mirrored HTML pages
- extraction includes `a`, `link`, `img`, `script`, and CSS `url(...)` references in mirrored HTML content
- reject patterns support extension shorthand forms (`gif`, `.gif`, `*.gif`)

## Docker/CI Troubleshooting
Common prerequisites for full validation pipelines:
- install full Go toolchain (matching project version policy)
- ensure CGO toolchain is present for race builds:
  - Linux: `gcc` (or compatible C compiler)
- ensure coverage tooling is available in the Go install
- run tests inside the Linux Docker image used for deployment parity

Common issues:
- `go test -race ./...` fails with missing compiler:
  - install `gcc` and rerun with `CGO_ENABLED=1`
- coverage instrumentation failures:
  - verify Go installation includes coverage tooling
  - rerun in CI/Docker base image with validated toolchain
