#!/bin/sh
set -eu

if [ "$#" -ne 1 ] || [ ! -f "$1/msedgewebview2.exe" ]; then
    printf '%s\n' "用法: sh build-windows.sh <已解包的 WebView2 Fixed Version x64 目录>" >&2
    exit 1
fi

runtime=$(cd "$1" && pwd)
cd "$(dirname "$0")"
output="$PWD"
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT

# overlay 将仅在构建时使用的内置运行时映射到 go:embed。
(cd "$runtime" && zip -q -r "$stage/runtime.zip" .)
python3 - "$stage" "$output" <<'PY'
import json
import sys
from pathlib import Path

stage, output = map(Path, sys.argv[1:])
mapping = {str(output / ".webview2-runtime.zip"): str(stage / "runtime.zip")}
(stage / "overlay.json").write_text(json.dumps({"Replace": mapping}))
PY

GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -overlay "$stage/overlay.json" \
    -tags standalone -trimpath -ldflags="-H=windowsgui -s -w" -o dota-shuffle.exe .
