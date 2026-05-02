# Development Guide

This guide is intended for developers who want to contribute to Distribyted or build it from source.

## Prerequisites

- **Go**: Version 1.21 or higher.
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
This runs the application using the example configuration file.

### 3. Running Tests
```bash
make test
```
To run tests with the race detector enabled:
```bash
make test-race
```

## Directory Structure

- `cmd/distribyted/`: The main entry point of the application.
- `fs/`: Core Virtual Filesystem (VFS) implementation.
    - `torrent.go`: Mapping torrents to files.
    - `container.go`: The root aggregation filesystem.
    - `storage.go`: In-memory tree structure for file metadata.
- `torrent/`: Bridge to the `anacrolix/torrent` engine.
    - `client.go`: Torrent client initialization.
    - `server.go`: The "Server" mode implementation (Folder-to-Magnet).
- `fuse/`: FUSE handler using `cgofuse`.
- `webdav/`: WebDAV server implementation.
- `http/`: Web dashboard and API handlers.
- `config/`: Configuration parsing and model.

## Contribution Workflow

1. Fork the repository.
2. Create a new branch for your feature or bugfix.
3. Ensure your code follows existing patterns and is well-tested.
4. Run `make test` to ensure no regressions.
5. Submit a Pull Request.

## Cross-Platform Builds

Distribyted can be cross-compiled, but note that `cgofuse` requires CGO. You may need a cross-compiler (like `mingw-w64` for Windows or `osxcross` for macOS) if you are building from Linux.
