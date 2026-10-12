//go:build darwin

package main

import (
	"encoding/xml"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	agentLabel = "wiki.wowdb.recorder-uploader"
	gameBinary = ".app/Contents/MacOS/World of Warcraft"
	appName    = "World Recorder.app"
)

func defaultInstalls() []string {
	return []string{"/Applications/World of Warcraft"}
}

func gameRunning() (bool, error) {
	out, err := exec.Command("ps", "-axo", "comm=").Output()
	if err != nil {
		return false, err
	}
	for line := range strings.Lines(string(out)) {
		if strings.HasSuffix(strings.TrimSpace(line), gameBinary) {
			return true, nil
		}
	}
	return false, nil
}

func detachConsole() {}

// Finder starts an app with /dev/null as standard input, which is also a character device.
func hasTerminal() bool {
	stdin, err := os.Stdin.Stat()
	if err != nil || stdin.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	null, err := os.Stat(os.DevNull)
	return err == nil && !os.SameFile(stdin, null)
}

// bundle names what setup copies: the whole app when the program runs from one, else the binary.
func bundle(exe string) (source, name, inner string) {
	macOS := filepath.Dir(exe)
	contents := filepath.Dir(macOS)
	app := filepath.Dir(contents)
	if filepath.Base(macOS) == "MacOS" && filepath.Base(contents) == "Contents" && strings.HasSuffix(app, ".app") {
		return app, appName, filepath.Join("Contents", "MacOS", filepath.Base(exe))
	}
	return exe, "wowdb-recorder", ""
}

// The login item restart in register stops the old uploader.
func stopOthers(string) {}

func openBrowser(address string) error { return exec.Command("open", address).Run() }

func showDialog(text string, failed bool) {
	icon := "note"
	if failed {
		icon = "stop"
	}
	exec.Command("osascript", "-e", "on run argv", "-e", "activate",
		"-e", `display dialog (item 1 of argv) with title "World Recorder" buttons {"OK"} default button "OK" with icon `+icon,
		"-e", "end run", text).Run()
}

func agentPath() (string, error) {
	home, err := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", agentLabel+".plist"), err
}

func agentDomain() string { return "gui/" + strconv.Itoa(os.Getuid()) }

func register(exe string, args []string) error {
	path, err := agentPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, agentPlist(append([]string{exe}, args...)), 0o644); err != nil {
		return err
	}
	exec.Command("launchctl", "bootout", agentDomain(), path).Run()
	return exec.Command("launchctl", "bootstrap", agentDomain(), path).Run()
}

func unregister() error {
	path, err := agentPath()
	if err != nil {
		return err
	}
	exec.Command("launchctl", "bootout", agentDomain(), path).Run()
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func agentPlist(program []string) []byte {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>` + agentLabel + `</string>
	<key>ProgramArguments</key>
	<array>
`)
	for _, arg := range program {
		b.WriteString("\t\t<string>")
		xml.EscapeText(&b, []byte(arg))
		b.WriteString("</string>\n")
	}
	b.WriteString(`	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<false/>
</dict>
</plist>
`)
	return []byte(b.String())
}
