#!/bin/sh
# Builds dist/WorldRecorder-mac.zip (World Recorder.app), dist/wowdb-recorder-darwin (universal) and dist/WorldRecorderSetup.exe. Runs on macOS.
set -eu
cd "$(dirname "$0")"
version=$(sed -n 's/^## Version: //p' ../WorldRecorder/WorldRecorder.toc)
rm -rf dist addon
mkdir -p dist addon
cp -R ../WorldRecorder addon/
build() { CGO_ENABLED=0 GOOS=$1 GOARCH=$2 go build -trimpath -buildvcs=false -o "$3" .; }
build darwin arm64 dist/darwin-arm64
build darwin amd64 dist/darwin-amd64
build windows amd64 dist/WorldRecorderSetup.exe
lipo -create -output dist/wowdb-recorder-darwin dist/darwin-arm64 dist/darwin-amd64
rm dist/darwin-arm64 dist/darwin-amd64
codesign --force -s - dist/wowdb-recorder-darwin
app="dist/World Recorder.app"
mkdir -p "$app/Contents/MacOS"
cp dist/wowdb-recorder-darwin "$app/Contents/MacOS/wowdb-recorder"
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
codesign --force --deep -s - "$app"
(cd dist && ditto -c -k --keepParent "World Recorder.app" WorldRecorder-mac.zip)
