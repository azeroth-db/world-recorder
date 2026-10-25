//go:build windows

package main

import (
	"bytes"
	_ "embed"
	"os/exec"
	"syscall"
)

// build.sh puts the desktop app here before building.
//
//go:embed "control/World Recorder.exe"
var controlFile []byte

const controlName = "World Recorder.exe"

func detached(cmd *exec.Cmd) *exec.Cmd {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: detachedProcess | syscall.CREATE_NEW_PROCESS_GROUP}
	return cmd
}

func sameControl(current []byte) bool { return bytes.Equal(current, controlFile) }
