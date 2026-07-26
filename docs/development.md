# Development Guide

This guide is intended for developers who want to contribute to Distribyted or build it from source.

## Prerequisites

- **Go**: Version 1.26 or higher (see the `go` directive in `go.mod`).
- **FUSE Support**:
    - **Linux**: `libfuse-dev` installed.
    - **Windows**: [WinFsp](https://github.com/winfsp/winfsp) installed.
    - **macOS**: [macFUSE](https://osxfuse.github.io/) installed.
- **C Compiler**: Needed for `cgofuse` (CGO).

## Building from Source

The project uses a `Makefile` to simplify common tasks.

### 1. Build the Binary
```bash
make build
```
This will generate the binary in the `bin/` directory.

### 2. Run from Source
```bash
make run
```
This runs the application using `examples/conf_example.yaml`. If that file doesn't exist yet, distribyted generates it automatically from the built-in template (`web/templates/config_template.yaml`) on first run, including default `admin`/`admin` credentials for both the HTTP and WebDAV servers — fine for local development, but change them (or set `http.disable_auth: true`) before exposing the server beyond localhost. See [Configuration](./configuration.md) for details.

### 3. Running Tests

- `make test-short`: fast unit tests only (`-short -race`), no network access required. This is what CI runs across the full Linux/macOS/Windows matrix, and what you should run locally for quick iteration.
- `make test-race`: the full suite, including the network-touching integration tests under `internal/testenv` (spins up real torrent clients/trackers/seeders on localhost). CI only runs this on Linux. Expect this to take a few minutes.
- `make test`: like `test-race` but without the race detector.

Tests gated behind `testing.Short()` are skipped by `test-short`; look for `if testing.Short() { t.Skip(...) }` at the top of a test if you're not sure which lane it runs in.

## Directory Structure

All packages below live under `internal/`, except the entry point:

- `cmd/distribyted/`: The main entry point of the application.
- `internal/fs/`: Core Virtual Filesystem (VFS) implementation.
    - `torrent.go`: Mapping torrents to files.
    - `container.go`: The root aggregation filesystem.
    - `storage.go`: In-memory tree structure for file metadata.
- `internal/torrent/`: Bridge to the `anacrolix/torrent` engine.
    - `client.go`: Torrent client initialization.
    - `server.go`: The "Server" mode implementation (Folder-to-Magnet).
- `internal/fuse/`: FUSE handler using `cgofuse`.
- `internal/webdav/`: WebDAV server implementation.
- `internal/http/`: Web dashboard and API handlers, including session-based authentication (`auth.go`) and the login page.
- `internal/auth/`: Shared constant-time credential comparison, used by both the HTTP and WebDAV servers.
- `internal/config/`: Configuration parsing, defaults, and startup validation.

## Contribution Workflow

1. Fork the repository.
2. Create a new branch for your feature or bugfix.
3. Ensure your code follows existing patterns and is well-tested.
4. Run `make test-short` for quick feedback, and `make test-race` before submitting to also cover the integration tests.
5. Submit a Pull Request.

## Cross-Platform Builds

Distribyted can be cross-compiled, but note that `cgofuse` requires CGO. You may need a cross-compiler (like `mingw-w64` for Windows or `osxcross` for macOS) if you are building from Linux.
