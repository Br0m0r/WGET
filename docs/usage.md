# Usage

## Build
```bash
go build -o wget ./cmd/wget
```
Windows PowerShell:
```powershell
go build -o .\wget.exe .\cmd\wget
```

## Basic Commands
- Single URL download:
```bash
./wget https://example.com/file.bin
```
- Save as specific name:
```bash
./wget -O file.bin https://example.com/file.bin
```
- Save under directory:
```bash
./wget -P /data/downloads https://example.com/file.bin
```
- Overwrite existing file:
```bash
./wget --force -O file.bin https://example.com/file.bin
```
- Download URLs from file:
```bash
./wget -i urls.txt
```

## Rate Limiting
Global limiter across downloads (binary units):
```bash
./wget --rate-limit 500k https://example.com/file.bin
./wget --rate-limit 2MiB/s https://example.com/file.bin
```

## Background Mode
```bash
./wget -B https://example.com/file.bin
```
Behavior:
- returns immediately
- redirects both stdout/stderr to `wget-log`
- creates `wget.pid`

## Mirror Mode
```bash
./wget --mirror https://example.com
```

Options:
- reject patterns:
```bash
./wget --mirror -R .jpg,*.gif https://example.com
```
`-R/--reject` accepts extension forms like `gif`, `.gif`, and `*.gif`.
- exclude directories:
```bash
./wget --mirror -X /admin,/tmp https://example.com
```
- convert links for mirrored HTML:
```bash
./wget --mirror --convert-links https://example.com
```
- strict robots mode:
```bash
./wget --mirror --strict-robots https://example.com
```

## Logging
- default format: human-readable
- optional JSON:
```bash
./wget --log-format json https://example.com/file.bin
```
- debug and trace:
```bash
./wget --debug https://example.com/file.bin
./wget --trace https://example.com/file.bin
```

## Proxy Support
Uses `HTTP_PROXY` and `HTTPS_PROXY` automatically.

## Offline Mirror Preview
When testing mirrored sites locally, serve from inside the mirrored domain directory:
- good root: `./example.com`
- bad root: repository root containing many mirrored domains

If you use Live Server, open the mirrored domain folder as the workspace root before starting the server.
