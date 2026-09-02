#!/bin/sh
# Records the README GIFs against a scratch engine seeded with the IRSA demo.
set -e
cd "$(dirname "$0")/../.."
make build >/dev/null
rm -rf .demorun && mkdir -p .demorun/out
cp testdata/irsa.cf.yaml .demorun/doc.cf.yaml
./bin/cf serve --addr 127.0.0.1:8086 --blueprint .demorun/doc.cf.yaml --out .demorun/out --lock .demorun/.cf.lock &
PID=$!
trap 'kill $PID 2>/dev/null' EXIT
sleep 1
node scripts/record-demos/record.js
