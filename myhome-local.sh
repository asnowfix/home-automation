#!/usr/bin/env bash
# myhome-local.sh — Bootstrap and run myhome from a local checkout (main tree or any git worktree).
#
# Generates the gitignored assets/code that `go build`/`go run` require (UI static
# assets, pool defaults) — skipping each step if already present — then execs
# `go run ./myhome ctl`. Each git worktree has its own untracked files, so a fresh
# worktree needs these regenerated even if the main checkout already has them.
#
# Usage:
#   ./myhome-local.sh [ctl-args...]
#   ./myhome-local.sh mcp                          # MCP stdio server (default if no args)
#   ./myhome-local.sh shelly script upload ...
#
#   MYHOME_MQTT_BROKER=tcp://192.168.1.2:1883 ./myhome-local.sh mcp
#
# This is the command referenced by .mcp.json so contributors get a working MCP
# server regardless of whether they're in the main tree or a worktree.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$REPO_ROOT"

BROKER="${MYHOME_MQTT_BROKER:-tcp://192.168.1.2:1883}"

[ -f internal/myhome/ui/static/alpine.min.js ] || go generate ./internal/myhome/ui/...
[ -f myhome/ctl/pool/pool_defaults_generated.go ] || go generate ./myhome/ctl/pool

if [ "$#" -eq 0 ]; then
  set -- mcp
fi

exec go run ./myhome ctl --instance local --mqtt-broker "$BROKER" "$@"
