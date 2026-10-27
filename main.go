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

	p := tea.NewProgram(newModel(cfg, NewClient(cfg.Host, token, fromGlab)), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fail(err)
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

Options:
  --config <path>   settings file to use
  -h, --help, --usage
                    show this help
  --version         print the version

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
