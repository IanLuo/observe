#!/usr/bin/env bash
# Build the UI: type-check with tsc, then bundle with esbuild into the directory
# internal/serve embeds. Both tools come from the dev shell in flake.nix; the nix
# package runs the same two steps, so this script is the single definition of the
# build and the committed app.js is only a convenience for `go build`.
set -euo pipefail
cd "$(dirname "$0")"

tsc --noEmit -p tsconfig.json
esbuild main.ts --bundle --minify --target=es2022 --format=iife \
  --outfile=../internal/serve/static/app.js \
  --banner:js='/* generated from web/ by web/build.sh — edit web/, not this file */'
