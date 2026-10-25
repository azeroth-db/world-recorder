package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
)

// The desktop app (tools/world-recorder/desktop) is this program's window and tray icon. It reads the same loopback
// API; its commands carry the token this program writes at start, which only the player's own account can read.

func (s *state) writeControlToken() (string, error) {
	token := make([]byte, 32)
	rand.Read(token)
	text := hex.EncodeToString(token)
	return text, s.write("control-token", []byte(text+"\n"))
}

func (d *daemon) controlled(header string) bool {
	return d.token != "" && subtle.ConstantTimeCompare([]byte(header), []byte(d.token)) == 1
}

// startControl starts the desktop app in the tray; with window it opens the window. The app runs once per user, so
// a start while it runs only shows the window.
func (d *daemon) startControl(window bool) {
	app, err := controlApp(d.exe)
	if err != nil {
		d.s.log("control result=failed: %v", pathless(err))
		return
	}
	args := []string{"--api", apiAddress, "--state", d.s.dir, "--origin", d.origin, "--program", d.exe}
	if !window {
		args = append(args, "--tray")
	}
	if err := detached(exec.Command(app, args...)).Start(); err != nil {
		d.s.log("control result=failed: %v", pathless(err))
	}
}

// openControl opens the window of the installed program's app from setup, which runs outside the daemon.
func (s *state) openControl(exe, origin string) error {
	app, err := controlApp(exe)
	if err != nil {
		return err
	}
	return detached(exec.Command(app, "--api", apiAddress, "--state", s.dir, "--origin", origin, "--program", exe)).Start()
}

// controlApp is the desktop app beside this program: in World Recorder.app's Contents/MacOS, beside the bare
// program or beside the Windows program. This program carries it and writes it there when the copy differs, so every
// install gets the app this program was built with. A running older app is renamed aside, which both systems allow;
// it yields to the new one when that starts.
func controlApp(exe string) (string, error) {
	app := filepath.Join(filepath.Dir(exe), controlName)
	if current, err := os.ReadFile(app); err == nil && sameControl(current) {
		return app, nil
	}
	if err := os.WriteFile(app+".new", controlFile, 0o755); err != nil {
		return "", err
	}
	old, err := swapIn(app+".new", app)
	if err != nil {
		return "", errors.Join(err, os.Remove(app+".new"))
	}
	os.Remove(old)
	return app, nil
}
