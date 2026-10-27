package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Host            string        `yaml:"host"`
	Token           string        `yaml:"token"`
	Projects        []string      `yaml:"projects"`
	RefreshInterval time.Duration `yaml:"refresh_interval"`
	ShowLabels      []string      `yaml:"show_labels"`
	HideLabels      []string      `yaml:"hide_labels"`
	Sort            string        `yaml:"sort"`

	showLabels []*regexp.Regexp
	hideLabels []*regexp.Regexp
	sort       sortOrder
}

// visibleLabels keeps the labels matching a show_labels pattern (all of them when there are
// none), then drops those matching a hide_labels pattern.
func (c Config) visibleLabels(labels []Label) []Label {
	var out []Label
	for _, l := range labels {
		if (len(c.showLabels) == 0 || matchAny(c.showLabels, l.Title)) && !matchAny(c.hideLabels, l.Title) {
			out = append(out, l)
		}
	}
	return out
}

func matchAny(res []*regexp.Regexp, s string) bool {
	for _, re := range res {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}

func compilePatterns(path, key string, patterns []string) ([]*regexp.Regexp, error) {
	var res []*regexp.Regexp
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("%s: %s: %w", path, key, err)
		}
		res = append(res, re)
	}
	return res, nil
}

const defaultConfig = `# tglab settings

# GitLab host (without scheme).
host: gitlab.com

# Projects to watch (full paths). Leave empty to show your open MRs across all projects.
projects: []
#  - my-group/my-project

# How often to refresh, e.g. 30s, 1m, 5m.
refresh_interval: 60s

# Labels to show: regular expressions matched against the label name. When set, only
# matching labels are shown; when empty, all are. Quote them, since a trailing ":" is
# YAML syntax.
show_labels: []
#  - "^autoreview::"

# Labels to hide, applied after show_labels.
hide_labels: []
#  - "^platform::"

# Starting row order: updated, mr, project, author, title, pipeline, approvals, threads or
# status, optionally followed by asc or desc; or none, for the order GitLab sends. s and S
# change it while tglab runs.
sort: updated desc

# Personal access token (read_api scope). Optional: if empty, GITLAB_TOKEN is used,
# then the token stored by glab ("glab auth login").
# token: glpat-...
`

func defaultConfigPath() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "tglab", "config.yaml")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "tglab", "config.yaml")
}

// resolveConfigPath picks an explicit path, then ./tglab.yaml, then the user config dir.
func resolveConfigPath(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if _, err := os.Stat("tglab.yaml"); err == nil {
		return "tglab.yaml"
	}
	return defaultConfigPath()
}

// loadConfig reads the settings file, creating it with defaults if it doesn't exist.
func loadConfig(path string) (Config, bool, error) {
	created := false
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return Config{}, false, err
		}
		if err := os.WriteFile(path, []byte(defaultConfig), 0o600); err != nil {
			return Config{}, false, err
		}
		data, created = []byte(defaultConfig), true
	} else if err != nil {
		return Config{}, false, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, false, fmt.Errorf("parse %s: %w", path, err)
	}
	if cfg.showLabels, err = compilePatterns(path, "show_labels", cfg.ShowLabels); err != nil {
		return Config{}, false, err
	}
	if cfg.hideLabels, err = compilePatterns(path, "hide_labels", cfg.HideLabels); err != nil {
		return Config{}, false, err
	}
	if cfg.sort, err = parseSort(cfg.Sort); err != nil {
		return Config{}, false, fmt.Errorf("%s: %w", path, err)
	}
	if cfg.Host == "" {
		cfg.Host = "gitlab.com"
	}
	cfg.Host = strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(cfg.Host, "https://"), "http://"), "/")
	if cfg.RefreshInterval <= 0 {
		cfg.RefreshInterval = 60 * time.Second
	}
	if cfg.RefreshInterval < 10*time.Second {
		cfg.RefreshInterval = 10 * time.Second
	}
	return cfg, created, nil
}

// resolveToken uses the config token, then GITLAB_TOKEN, then glab's stored token. fromGlab
// reports the last case: glab's OAuth tokens expire, so the client has to keep re-reading it.
func resolveToken(cfg Config) (token string, fromGlab bool, err error) {
	if cfg.Token != "" {
		return cfg.Token, false, nil
	}
	if t := os.Getenv("GITLAB_TOKEN"); t != "" {
		return t, false, nil
	}
	if t, err := glabToken(cfg.Host); err == nil {
		return t, true, nil
	}
	return "", false, fmt.Errorf("no GitLab token: set `token` in the config, export GITLAB_TOKEN, or run `glab auth login --hostname %s`", cfg.Host)
}

// glabToken returns the token glab currently stores for host.
func glabToken(host string) (string, error) {
	out, err := exec.Command("glab", "config", "get", "token", "--host", host).Output()
	t := strings.TrimSpace(string(out))
	if err != nil || t == "" {
		return "", fmt.Errorf("glab has no token for %s", host)
	}
	return t, nil
}

// renewGlabToken gets glab to renew an expired OAuth token, which it does before any API call of
// its own, and returns the token it stores afterwards.
func renewGlabToken(ctx context.Context, host string) (string, error) {
	_ = exec.CommandContext(ctx, "glab", "api", "user", "--hostname", host).Run()
	return glabToken(host)
}
