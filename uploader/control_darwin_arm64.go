package main

import _ "embed"

// build.sh puts the desktop app's arm64 slice here before building, so each slice of the program carries only its own.
//
//go:embed "control/World Recorder-arm64"
var controlFile []byte
