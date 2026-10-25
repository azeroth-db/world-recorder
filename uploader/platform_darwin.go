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
	"syscall"
)

const (
	// bundleID is the app's CFBundleIdentifier in build.sh's Info.plist; Login Items settings name the agent after it.
	bundleID = "wiki.wowdb.recorder-uploader"
	appName  = "World Recorder.app"
)

// agentLabel is a variable only so a test build can run under a label of its own (-ldflags -X main.agentLabel=...).
var agentLabel = "wiki.wowdb.recorder-uploader"

func defaultInstalls() []string {
	return append(battleNetInstalls("/Users/Shared/Battle.net/Agent/product.db"), "/Applications/World of Warcraft")
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
	path, err := writeAgent(exe, args)
	if err != nil {
		return err
	}
	exec.Command("launchctl", "bootout", agentService()).Run()
	return exec.Command("launchctl", "bootstrap", agentDomain(), path).Run()
}

// agentService names the loaded job, which launchd finds even after its file was removed.
func agentService() string { return agentDomain() + "/" + agentLabel }

func loginItemExists() bool {
	path, err := agentPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

func writeAgent(exe string, args []string) (string, error) {
	path, err := agentPath()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	return path, os.WriteFile(path, agentPlist(append([]string{exe}, args...)), 0o644)
}

// setLoginItem writes or removes the agent file without loading or unloading it, so the running program stays;
// launchd reads the change at the next login.
func setLoginItem(on bool, exe string, args []string) error {
	if on {
		_, err := writeAgent(exe, args)
		return err
	}
	path, err := agentPath()
	if err == nil {
		err = os.Remove(path)
	}
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// swapProgram puts the app, or the bare binary, from the release archive in place of the installed one and
// returns where the previous one is kept.
func swapProgram(archive, exe string) (string, error) {
	temp, err := os.MkdirTemp(filepath.Dir(archive), "update-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(temp)
	if err := extractZip(archive, temp); err != nil {
		return "", err
	}
	fresh, target := filepath.Join(temp, appName), programPath(exe)
	if _, _, inner := bundle(exe); inner == "" {
		fresh = filepath.Join(fresh, "Contents", "MacOS", "wowdb-recorder")
	}
	if err := errors.Join(os.RemoveAll(target+".new"), copyTree(fresh, target+".new")); err != nil {
		return "", err
	}
	return swapIn(target+".new", target)
}

// restartProgram restarts the login item through launchd, or, started any other way, runs the new file in place.
func restartProgram(exe string) error {
	if os.Getenv("XPC_SERVICE_NAME") == agentLabel {
		return exec.Command("launchctl", "kickstart", "-k", agentDomain()+"/"+agentLabel).Run()
	}
	return syscall.Exec(exe, os.Args, os.Environ())
}

// unregister removes the agent file, then unloads the job by its label; when this program is that job,
// unloading ends it.
func unregister() error {
	path, err := agentPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	exec.Command("launchctl", "bootout", agentService()).Run()
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
	<true/>
	<key>ProcessType</key>
	<string>Background</string>
	<key>AssociatedBundleIdentifiers</key>
	<string>` + bundleID + `</string>
</dict>
</plist>
`)
	return []byte(b.String())
}

// computerName is the name Finder and Sharing show, such as "Eugene's MacBook Pro".
func computerName() string {
	out, err := exec.Command("scutil", "--get", "ComputerName").Output()
	if name := strings.TrimSpace(string(out)); err == nil && name != "" {
		return name
	}
	name, _ := os.Hostname()
	return strings.TrimSuffix(name, ".local")
}
