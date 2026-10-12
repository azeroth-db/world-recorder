#!/bin/sh
# Installs the World Recorder addon into the WoW Forever client and the uploader as a login item, then links this Mac to the player's wowdb.wiki account.
set -eu
base=https://wowdb.wiki/assets/world-recorder/1.2.1
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
curl -fsSL "$base/wowdb-recorder-darwin" -o "$work/download"
chmod 755 "$work/download"
mv "$work/download" "$work/wowdb-recorder"
if [ -n "${WOW_DIR:-}" ]; then "$work/wowdb-recorder" install --wow "$WOW_DIR"; else "$work/wowdb-recorder" install; fi
