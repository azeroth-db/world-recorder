package main

import _ "embed"

// build.sh puts the desktop app's x86_64 slice here before building, so each slice of the program carries only its own.
//
//go:embed "control/World Recorder-x86_64"
var controlFile []byte
