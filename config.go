package main

import (
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
	HideLabels      []string      `yaml:"hide_labels"`

	hideLabels []*regexp.Regexp
}

// visibleLabels drops the labels matching any hide_labels pattern.
func (c Config) visibleLabels(labels []Label) []Label {
	var out []Label
next:
	for _, l := range labels {
		for _, re := range c.hideLabels {
			if re.MatchString(l.Title) {
				continue next
			}
		}
		out = append(out, l)
	}
	return out
}

const defaultConfig = `# tglab settings

# GitLab host (without scheme).
host: gitlab.com

# Projects to watch (full paths). Leave empty to show your open MRs across all projects.
projects: []
#  - my-group/my-project

# How often to refresh, e.g. 30s, 1m, 5m.
refresh_interval: 60s

# Labels to hide: regular expressions matched against the label name. Quote them,
# since a trailing ":" is YAML syntax.
hide_labels: []
#  - "^platform::"

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
	for _, p := range cfg.HideLabels {
		re, err := regexp.Compile(p)
		if err != nil {
			return Config{}, false, fmt.Errorf("%s: hide_labels: %w", path, err)
		}
		cfg.hideLabels = append(cfg.hideLabels, re)
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

// resolveToken uses the config token, then GITLAB_TOKEN, then glab's stored token.
func resolveToken(cfg Config) (string, error) {
	if cfg.Token != "" {
		return cfg.Token, nil
	}
	if t := os.Getenv("GITLAB_TOKEN"); t != "" {
		return t, nil
	}
	out, err := exec.Command("glab", "config", "get", "token", "--host", cfg.Host).Output()
	if t := strings.TrimSpace(string(out)); err == nil && t != "" {
		return t, nil
	}
	return "", fmt.Errorf("no GitLab token: set `token` in the config, export GITLAB_TOKEN, or run `glab auth login --hostname %s`", cfg.Host)
}
