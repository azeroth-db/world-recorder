package main

import (
	"flag"
	"fmt"
	"maps"
	"os"
	"strings"
	"time"
)

const (
	defaultEndpoint = "https://wowdb.wiki/api/contrib/recorder"
	pollInterval    = 10 * time.Second
)

type options struct {
	wow      string
	endpoint string
	once     bool
	args     []string
}

func main() {
	command, opts := parseArgs(os.Args[1:])
	if err := dispatch(command, opts); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func parseArgs(args []string) (string, options) {
	command := "run"
	if len(args) == 0 && !hasTerminal() {
		command = "setup"
		detachConsole()
	}
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		command, args = args[0], args[1:]
	}
	flags := flag.NewFlagSet("wowdb-recorder", flag.ExitOnError)
	var opts options
	flags.StringVar(&opts.wow, "wow", "", "World of Warcraft install directory")
	flags.StringVar(&opts.endpoint, "endpoint", defaultEndpoint, "URL the saves are sent to")
	flags.BoolVar(&opts.once, "once", false, "make one pass, then exit")
	flags.Parse(args)
	if flags.NArg() > 0 {
		fmt.Fprintln(os.Stderr, "commands: setup, install, link, run, status, uninstall")
		os.Exit(2)
	}
	flags.Visit(func(f *flag.Flag) {
		if f.Name != "once" {
			opts.args = append(opts.args, "--"+f.Name+"="+f.Value.String())
		}
	})
	return command, opts
}

func dispatch(command string, opts options) error {
	if command == "uninstall" {
		return unregister()
	}
	s, err := openState()
	if err != nil {
		return pathless(err)
	}
	switch command {
	case "setup":
		return s.setup(opts, true)
	case "install":
		return s.setup(opts, false)
	case "link":
		return s.link(opts.endpoint)
	case "status":
		return s.status(opts.wow)
	case "run":
		return s.run(opts)
	}
	return fmt.Errorf("unknown command %q", command)
}

func (s *state) run(opts options) error {
	if opts.once {
		failed, err := s.pass(opts.wow, opts.endpoint)
		if err == nil && failed > 0 {
			err = fmt.Errorf("%d save(s) not sent", failed)
		}
		return err
	}
	detachConsole()
	return s.watch(opts)
}

func (s *state) watch(opts options) error {
	seen, key := snapshot(findSaves(installs(opts.wow))), s.readKey()
	if _, err := s.pass(opts.wow, opts.endpoint); err != nil {
		return err
	}
	wasRunning := s.gameRunning()
	for range time.Tick(pollInterval) {
		running, current, currentKey := s.gameRunning(), snapshot(findSaves(installs(opts.wow))), s.readKey()
		if running && !wasRunning || !maps.Equal(current, seen) || currentKey != key {
			if _, err := s.pass(opts.wow, opts.endpoint); err != nil {
				return err
			}
		}
		wasRunning, seen, key = running, current, currentKey
	}
	return nil
}

func (s *state) gameRunning() bool {
	running, err := gameRunning()
	if err != nil {
		s.log("process list result=failed: %v", err)
	}
	return running
}

func (s *state) status(wow string) error {
	fmt.Println("install id:", s.install)
	fmt.Println("linked:", map[bool]string{true: "yes", false: "no"}[s.readKey() != ""])
	saves := findSaves(installs(wow))
	if len(saves) == 0 {
		fmt.Println("no World Recorder saves found")
	}
	for i, sv := range saves {
		fmt.Printf("%d. %s: %s\n", i+1, sv.flavor, s.saveStatus(sv))
	}
	return nil
}

func (s *state) saveStatus(sv save) string {
	c, err := convert(sv)
	switch {
	case err != nil:
		return "unreadable: " + err.Error()
	case s.sent(sv.path, s.readKey(), c.hash):
		return "sent"
	}
	return "not sent"
}
