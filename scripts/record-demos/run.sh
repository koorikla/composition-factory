#!/bin/sh
# Records the README GIFs against a scratch engine seeded with the IRSA demo.
set -e
cd "$(dirname "$0")/../.."
TOPLEVEL=$(git rev-parse --show-toplevel 2>/dev/null || pwd)
HASH=$(node -e "const crypto=require('crypto'); console.log(crypto.createHash('sha256').update(process.argv[1]).digest('hex').slice(0, 8))" "$TOPLEVEL")
PORT_OFFSET=$(node -e "console.log(parseInt('$HASH', 16) % 10000)")
PORT=${CF_DEMO_PORT:-$((28000 + PORT_OFFSET))}
SCRATCH_DIR=".demorun-${HASH}"

make build >/dev/null
rm -rf "$SCRATCH_DIR" && mkdir -p "$SCRATCH_DIR/out"
cp testdata/irsa.cf.yaml "$SCRATCH_DIR/doc.cf.yaml"
./bin/cf serve --addr "127.0.0.1:$PORT" --blueprint "$SCRATCH_DIR/doc.cf.yaml" --out "$SCRATCH_DIR/out" --lock "$SCRATCH_DIR/.cf.lock" &
PID=$!
trap 'kill $PID 2>/dev/null' EXIT
sleep 1
CF_DEMO_PORT=$PORT node scripts/record-demos/record.js

