package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// version is set at release build time with -ldflags "-X main.version=v1.2.3".
var version = "dev"

func main() {
	configFlag := flag.String("config", "", "path to the settings file")
	showVersion := flag.Bool("version", false, "print the version")
	update := flag.Bool("update", false, "update to the newest release, after asking")
	noUpdateCheck := flag.Bool("no-update-check", false, "don't look for newer releases")
	var help bool
	for _, name := range []string{"help", "h", "usage"} {
		flag.BoolVar(&help, name, false, "show this help")
	}
	flag.Usage = func() { printUsage(os.Stderr, *configFlag) }
	flag.Parse()
	if help {
		printUsage(os.Stdout, *configFlag)
		return
	}
	if *showVersion {
		fmt.Println("tglab", version)
		return
	}
	if *update {
		if err := updateCLI("tglab", version, os.Stdin, os.Stdout); err != nil {
			fail(err)
		}
		return
	}

	path := resolveConfigPath(*configFlag)
	cfg, created, err := loadConfig(path)
	if err != nil {
		fail(err)
	}
	if created {
		fmt.Fprintf(os.Stderr, "Created default settings at %s\n", path)
	}

	token, fromGlab, err := resolveToken(cfg)
	if err != nil {
		fail(err)
	}

	m := newModel(cfg, NewClient(cfg.Host, token, fromGlab))
	m.upd = newUpdater("tglab", version, *noUpdateCheck || os.Getenv("TGLAB_NO_UPDATE_CHECK") != "")
	final, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	if err != nil {
		fail(err)
	}
	if fm, ok := final.(model); ok && fm.upd.Restart != "" {
		fail(restart(fm.upd.Restart))
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "tglab:", err)
	os.Exit(1)
}

func printUsage(w io.Writer, configFlag string) {
	path := resolveConfigPath(configFlag)
	state := "exists"
	if _, err := os.Stat(path); err != nil {
		state = "missing, created with the defaults below on first run"
	}

	fmt.Fprintf(w, `tglab %s — keeps showing the GitLab merge requests you opened, or all open MRs in your projects.

Usage:
  tglab [--config <path>]
  tglab --help | --usage | --version
  tglab --update

Options:
  --config <path>   settings file to use
  --update          update to the newest release, after asking
  --no-update-check don't look for newer releases (or set TGLAB_NO_UPDATE_CHECK=1)
  -h, --help, --usage
                    show this help
  --version         print the version

tglab looks for a newer release at start and once a day; the status line shows one, and u
updates (after asking).

Settings file (YAML), first match wins:
  1. --config <path>
  2. ./tglab.yaml
  3. %s

  In use now: %s (%s)

Settings and their defaults:
%s
Token, first one set wins:
  1. "token" in the settings file (scope: read_api)
  2. the GITLAB_TOKEN environment variable
  3. the token stored by glab (glab auth login --hostname <host>)

Keys:
  up/down, j/k   select and move (up with nothing selected picks the last MR)
  enter, o       open the selected MR in the browser
  esc            clear the selection
  a              switch between my MRs (default) and all MRs in the projects
  s              sort by the next column (the header marks it ▲/▼), then none
  S              reverse the sort
  r              refresh now
  u              update to a newer release, when the status line shows one
  q, ctrl+c      quit
`, version, defaultConfigPath(), path, state, indent(defaultConfig, "  "))
}

func indent(s, prefix string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		if l != "" {
			lines[i] = prefix + l
		}
	}
	return strings.Join(lines, "\n") + "\n"
}
