#!/usr/bin/env bash
# forgego's setup_script, run before compiling on the macOS and Windows runners: cgofuse compiles
# against macFUSE's headers, and the Windows build, without cgo, loads WinFsp when it mounts.
set -euo pipefail

case "$RUNNER_OS" in
  macOS)
    brew update
    brew install --cask macfuse
    ;;
  Windows)
    pwsh -NoProfile -NonInteractive -File "$(dirname "$0")/winfsp.ps1"
    ;;
esac
