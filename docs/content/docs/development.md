---
title: Development
weight: 6
---

This guide is intended for developers who want to contribute to Distribyted or build it from source.

## Prerequisites

- **Go**: Version 1.26 or higher (see the `go` directive in `go.mod`).
- **FUSE Support**:
    - **Linux**: `libfuse-dev` installed.
    - **Windows**: [WinFsp](https://github.com/winfsp/winfsp) installed.
    - **macOS**: [macFUSE](https://osxfuse.github.io/) installed.
- **C Compiler**: Needed for `cgofuse` (CGO).

## Building from Source

Common tasks live in `Taskfile.yml` and run through [Task](https://taskfile.dev/), which the project pins along with its other tools in `.forgego/`, so nothing needs installing beyond Go:

```bash
go tool -modfile=.forgego/task/go.mod task --list
```

The examples below shorten that to `task`: alias it, or install Task.

### 1. Build the Binary
```bash
task build
```
This runs `go generate` and writes the binary to `bin/`, named for the version and platform.

### 2. Run from Source
```bash
task start
```
This runs the application using `examples/conf_example.yaml`, or the config path you pass after `--` (`task start -- my-config.yaml`). If that file doesn't exist yet, distribyted generates it automatically from the built-in template (`web/templates/config_template.yaml`) on first run, including default `admin`/`admin` credentials for both the HTTP and WebDAV servers — fine for local development, but change them (or set `http.disable_auth: true`) before exposing the server beyond localhost. See [Configuration](../configuration/) for details.

### 3. Running Tests

- `task test:short`: fast unit tests only (`-short`), no network access required. This is what CI runs across the full Linux/macOS/Windows matrix, and what you should run locally for quick iteration.
- `task test`: the full suite, including the network-touching integration tests under `internal/testenv` (spins up real torrent clients/trackers/seeders on localhost). CI only runs this on Linux. Expect this to take a few minutes.

Both write `coverage.out` and a JUnit `junit-report.xml`. CI runs them with the race detector; do the same locally with `GOFLAGS=-race task test`.

Tests gated behind `testing.Short()` are skipped by `test:short`; look for `if testing.Short() { t.Skip(...) }` at the top of a test if you're not sure which lane it runs in.

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
4. Run `task test:short` for quick feedback, and `task test` before submitting to also cover the integration tests. `task lint` formats the code and fixes what golangci-lint can.
5. Install the Git hooks once with `task hooks`: they format staged Go files on commit, lint on push, and check commit messages against Conventional Commits.
6. Submit a Pull Request.

## Cross-Platform Builds

Distribyted can be cross-compiled, but note that `cgofuse` requires CGO. You may need a cross-compiler (like `mingw-w64` for Windows or `osxcross` for macOS) if you are building from Linux.
