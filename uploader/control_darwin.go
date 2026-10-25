//go:build darwin

package main

import (
	"bytes"
	"debug/macho"
	"os/exec"
	"syscall"
)

const controlName = "World Recorder"

// detached starts the app in a session of its own, so it outlives a restart of this program.
func detached(cmd *exec.Cmd) *exec.Cmd {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd
}

// sameControl accepts this program's slice of the app, or the universal app World Recorder.app ships, which holds it.
func sameControl(current []byte) bool {
	if bytes.Equal(current, controlFile) {
		return true
	}
	fat, err := macho.NewFatFile(bytes.NewReader(current))
	if err != nil {
		return false
	}
	defer fat.Close()
	for _, arch := range fat.Arches {
		end := uint64(arch.Offset) + uint64(arch.Size)
		if end <= uint64(len(current)) && bytes.Equal(current[arch.Offset:end], controlFile) {
			return true
		}
	}
	return false
}
