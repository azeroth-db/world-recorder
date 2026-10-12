#!/bin/sh
# Builds dist/WorldRecorder-<version>.zip with the addon folder at its root, the layout players unpack into Interface/AddOns.
set -eu
cd "$(dirname "$0")"
./check-api.sh
version=$(sed -n 's/^## Version: //p' WorldRecorder/WorldRecorder.toc)
mkdir -p dist
rm -f "dist/WorldRecorder-$version.zip"
zip -qrX "dist/WorldRecorder-$version.zip" WorldRecorder
echo "dist/WorldRecorder-$version.zip"
