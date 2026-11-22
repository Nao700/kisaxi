#!/bin/sh
# Linux 上でビルドする場合 (Go 1.26 以降が必要)
set -e
cd "$(dirname "$0")/src"
go test ./...
mkdir -p ../dist
CGO_ENABLED=0 go build -trimpath -ldflags '-s -w' -o ../dist/KISAXI .
echo "built: dist/KISAXI"
