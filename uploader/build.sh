#!/bin/sh
# Builds dist/WorldRecorderSetup.exe. The macOS standalone app is retired (WOW-106).
# Cross-compilation still uses Qt host tools on this Mac; QT_WIN is the MinGW build for Windows
# (../desktop/README.mdx#build-qt).
set -eu
cd "$(dirname "$0")"
version=$(sed -n 's/^## Version: //p' ../WorldRecorder/WorldRecorder.toc)
qt=$(cd ../../.. && pwd)/.work/qt
QT_HOST=${QT_HOST:-$qt/mac}
QT_WIN=${QT_WIN:-$qt/win}
rm -rf dist control ../desktop/build
mkdir -p dist control
python3 ../desktop/strings.py
desktop() {
  out=../desktop/build/$1
  shift
  cmake -S ../desktop -B "$out" -G Ninja -DCMAKE_BUILD_TYPE=MinSizeRel -DAPP_VERSION="$version" "$@" >/dev/null
  cmake --build "$out" >/dev/null
}
desktop win -DCMAKE_TOOLCHAIN_FILE="$QT_WIN/lib/cmake/Qt6/qt.toolchain.cmake" -DQT_HOST_PATH="$QT_HOST"
x86_64-w64-mingw32-strip -o "control/World Recorder.exe" "../desktop/build/win/World Recorder.exe"
# The program carries the addon's version and installs the addon zip of the signed release.json.
# GOLDFLAGS lets a test build serve another port under another login item (-X main.apiAddress=..., main.agentLabel=...).
build() { CGO_ENABLED=0 GOOS=$1 GOARCH=$2 go build -trimpath -buildvcs=false -ldflags "-X main.programVersion=$version ${GOLDFLAGS:-}" -o "$3" .; }
build windows amd64 dist/WorldRecorderSetup.exe
