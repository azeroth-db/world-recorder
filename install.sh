#!/bin/sh
# Installs the World Recorder addon into the WoW Forever client and the uploader as a login item, then links this Mac to the player's Azeroth DB account.
set -eu
path=/assets/world-recorder/1.7.7
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
# The program comes from the app the site serves, the same universal binary as double-clicking it runs.
# azeroth.wiki first; wowdb.wiki serves the same files when azeroth.wiki does not answer.
curl -fsSL "https://azeroth.wiki$path/WorldRecorder-mac.zip" -o "$work/WorldRecorder-mac.zip" ||
  curl -fsSL "https://wowdb.wiki$path/WorldRecorder-mac.zip" -o "$work/WorldRecorder-mac.zip"
ditto -x -k "$work/WorldRecorder-mac.zip" "$work"
program="$work/World Recorder.app/Contents/MacOS/wowdb-recorder"
if [ -n "${WOW_DIR:-}" ]; then "$program" install --wow "$WOW_DIR"; else "$program" install; fi
