# WGET (Go)

Production-grade, wget-like CLI implemented in Go for Linux-based Docker deployment (cross-platform compatible).

## Features
- Single URL download with streaming writes
- Output controls: `-O` (filename), `-P` (directory), `--force`
- Resume support with `<target>.part`
- Retry policy: retryable `408`, `429`, `5xx` with exponential backoff + jitter
- Global rate limiting (`--rate-limit`, binary units)
- Background mode (`-B`) with `wget-log` and `wget.pid`
- Batch downloads from input file (`-i`)
- Recursive mirroring (`--mirror`) with:
  - same registrable domain scope
  - robots support (default non-fatal fetch/parse failures)
  - optional strict robots mode (`--strict-robots`)
  - reject/exclude filters (`-R`, `-X`) including extension forms like `gif`, `.gif`, `*.gif`
  - link conversion (`--convert-links`)
  - CSS `url(...)` asset discovery from mirrored HTML
- Structured logging via `log/slog` (human default, JSON optional)

## Quick Start
Linux/macOS:
```bash
go build -o wget ./cmd/wget
./wget https://example.com/file.bin
```

Windows (PowerShell):
```powershell
go build -o .\wget.exe .\cmd\wget
.\wget.exe "https://example.com/file.bin"
```

Note: on Windows, use `.\wget.exe` (not `./wget`), otherwise the shell may prompt you to choose an app to open the file.

Examples:
```bash
./wget -O file.bin -P /data https://example.com/file.bin
./wget --rate-limit 2MiB/s https://example.com/file.bin
./wget -B https://example.com/file.bin
./wget -i urls.txt
./wget --mirror --convert-links https://example.com
```

For offline mirrored browsing, serve from the mirrored domain folder root (for example `example.com/`), not the project root.

## Testing
```bash
go test ./...
go test -tags=stress ./...
```

Policy scripts:
- `scripts/test.sh`
- `scripts/test.ps1`

## Documentation
- Architecture: `docs/architecture.md`
- Usage: `docs/usage.md`
- Developer guide: `docs/developer.md`
- Testing + profiling: `docs/testing.md`

## Notes
- Module Go version is pinned to `go 1.24.0` in `go.mod`.
- Full race checks require CGO and a C compiler (`gcc` in Linux environments).
