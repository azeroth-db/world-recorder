#!/bin/sh
# Builds dist/WorldRecorder-mac.zip (World Recorder.app), dist/wowdb-recorder-darwin (universal) and dist/WorldRecorderSetup.exe. Runs on macOS.
# The desktop app (../desktop) links a static Qt: QT_MAC is a universal macOS build, QT_WIN a MinGW build for Windows
# (../desktop/README.mdx#build-qt).
set -eu
cd "$(dirname "$0")"
version=$(sed -n 's/^## Version: //p' ../WorldRecorder/WorldRecorder.toc)
qt=$(cd ../../.. && pwd)/.work/qt
QT_MAC=${QT_MAC:-$qt/mac}
QT_WIN=${QT_WIN:-$qt/win}
rm -rf dist addon.zip control ../desktop/build
mkdir -p dist control
# The program installs the same complete addon the site publishes: package.sh's validated archive.
cp "../dist/WorldRecorder-$version.zip" addon.zip || { echo "Run ../package.sh first: ../dist/WorldRecorder-$version.zip is missing" >&2; exit 1; }
python3 ../desktop/strings.py
desktop() {
  out=../desktop/build/$1
  shift
  cmake -S ../desktop -B "$out" -G Ninja -DCMAKE_BUILD_TYPE=MinSizeRel -DAPP_VERSION="$version" "$@" >/dev/null
  cmake --build "$out" >/dev/null
}
desktop mac -DCMAKE_PREFIX_PATH="$QT_MAC" -DCMAKE_OSX_ARCHITECTURES="arm64;x86_64" -DCMAKE_OSX_DEPLOYMENT_TARGET=12.0
desktop win -DCMAKE_TOOLCHAIN_FILE="$QT_WIN/lib/cmake/Qt6/qt.toolchain.cmake" -DQT_HOST_PATH="$QT_MAC"
x86_64-w64-mingw32-strip -o "control/World Recorder.exe" "../desktop/build/win/World Recorder.exe"
# World Recorder.app ships the universal app; each slice of the program embeds its own slice of it, byte for byte, so
# the program finds the bundle's copy current and never rewrites it.
strip -x -o "control/World Recorder" "../desktop/build/mac/World Recorder"
codesign --force -s - "control/World Recorder"
for arch in arm64 x86_64; do
  lipo -thin $arch -output "control/World Recorder-$arch" "control/World Recorder"
done
# GOLDFLAGS lets a test build serve another port under another login item (-X main.apiAddress=..., main.agentLabel=...).
build() { CGO_ENABLED=0 GOOS=$1 GOARCH=$2 go build -trimpath -buildvcs=false -ldflags "${GOLDFLAGS:-}" -o "$3" .; }
build darwin arm64 dist/darwin-arm64
build darwin amd64 dist/darwin-amd64
build windows amd64 dist/WorldRecorderSetup.exe
lipo -create -output dist/wowdb-recorder-darwin dist/darwin-arm64 dist/darwin-amd64
rm dist/darwin-arm64 dist/darwin-amd64
codesign --force -s - dist/wowdb-recorder-darwin
app="dist/World Recorder.app"
mkdir -p "$app/Contents/MacOS"
cp dist/wowdb-recorder-darwin "$app/Contents/MacOS/wowdb-recorder"
cp "control/World Recorder" "$app/Contents/MacOS/World Recorder"
cat > "$app/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleExecutable</key>
	<string>wowdb-recorder</string>
	<key>CFBundleIdentifier</key>
	<string>wiki.wowdb.recorder-uploader</string>
	<key>CFBundleName</key>
	<string>World Recorder</string>
	<key>CFBundlePackageType</key>
	<string>APPL</string>
	<key>CFBundleInfoDictionaryVersion</key>
	<string>6.0</string>
	<key>CFBundleShortVersionString</key>
	<string>$version</string>
	<key>CFBundleVersion</key>
	<string>$version</string>
	<key>LSUIElement</key>
	<true/>
</dict>
</plist>
PLIST
codesign --force -s - "$app"
(cd dist && ditto -c -k --keepParent "World Recorder.app" WorldRecorder-mac.zip)
