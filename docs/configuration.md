# Configuration

tglab reads one YAML settings file. This page covers where that file lives, what each setting
does, and examples for common setups.

## Where the settings file lives

The first of these that exists is used:

1. the path given with `--config <path>`
2. `tglab.yaml` in the current directory
3. `~/.config/tglab/config.yaml`, or `$XDG_CONFIG_HOME/tglab/config.yaml` when `XDG_CONFIG_HOME`
   is set

If none exists, the file from step 3 is created with the defaults on first run, readable only by
you (`0600`). The defaults are in [`config.example.yaml`](../config.example.yaml).

To see which file is in use right now, run:

```sh
tglab --help
```

It prints the lookup order, the file in use, whether it exists, and the full default settings.

## Settings

| Setting | Type | Default |
|---|---|---|
| [`host`](#host) | string | `gitlab.com` |
| [`projects`](#projects) | list of strings | `[]` |
| [`refresh_interval`](#refresh_interval) | duration | `60s` |
| [`show_labels`](#show_labels-and-hide_labels) | list of regular expressions | `[]` |
| [`hide_labels`](#show_labels-and-hide_labels) | list of regular expressions | `[]` |
| [`token`](#token) | string | none |

Settings tglab doesn't recognise are ignored, so a typo in a name means that setting keeps its
default.

### `host`

The GitLab host, without a scheme. tglab talks to `https://<host>/api/graphql`.

- A leading `https://` or `http://` and a trailing `/` are removed, so `https://gitlab.example.com/`
  works too.
- Only HTTPS is used, even if you write `http://`.

### `projects`

Full paths of the projects to watch, as they appear in the project URL after the host, e.g.
`my-group/my-subgroup/my-project`.

- **One or more projects:** each refresh fetches every open MR in them. The default view shows the
  ones you opened, and `a` switches to all of them.
- **Empty (`[]`):** tglab shows your own open MRs across every project on the host. There's no
  "all MRs" view in this mode; pressing `a` shows an error in the status line.
- With exactly one project, the Project column is hidden, since it would repeat the same path on
  every row. With none or several, it's shown.

### `refresh_interval`

How often to fetch MRs again, as a Go duration: `30s`, `1m`, `2m30s`, `1h`.

- Missing, `0` or negative means `60s`.
- Anything under `10s` is raised to `10s`, to stay well within GitLab's API rate limits.
- `r` refreshes immediately at any time.

### `show_labels` and `hide_labels`

Both are lists of regular expressions, matched against each label's full name, such as
`autoreview::clean`. They control which labels appear in the Labels column; they never hide MRs.

1. If `show_labels` has any patterns, only labels matching **at least one** of them are kept. If
   it's empty, every label is kept.
2. Then labels matching **any** `hide_labels` pattern are removed.

So `hide_labels` can remove labels that `show_labels` let through, but never brings back one that
`show_labels` left out.

The patterns use [Go's regular expression syntax](https://pkg.go.dev/regexp/syntax):

- A pattern matches anywhere in the name unless you anchor it. `platform` also matches
  `cross-platform`; `^platform::` only matches labels starting with `platform::`.
- Matching is case-sensitive. Add `(?i)` at the start to ignore case: `(?i)^bug$`.
- These are regular expressions, not globs. `priority*` means "`priorit` followed by any number of
  `y`"; write `^priority` for "starts with priority".
- **Quote every pattern.** In YAML, a value ending in `:` (like `- ^platform::`) is read as a map,
  not a string, and tglab fails to start.
- An invalid pattern stops tglab at startup with an error naming the setting and the file.

Labels hidden this way also don't count as changes for the `●` marker.

### `token`

A GitLab personal access token with the `read_api` scope. tglab only reads from GitLab; it never
needs write access.

The token is taken from the first of these that is set:

1. `token` in the settings file
2. the `GITLAB_TOKEN` environment variable
3. the token stored by [`glab`](https://gitlab.com/gitlab-org/cli) for the same host
   (`glab auth login --hostname <host>`)

If you're logged in with `glab`, leave `token` out. If you do put a token in the file, keep the file
private: `tglab.yaml` is in this repository's `.gitignore` for that reason.

## Examples

### Your MRs in one project

```yaml
host: gitlab.com
projects:
  - my-group/my-project
```

Starts on your open MRs in `my-group/my-project`; `a` shows everyone's.

### Your MRs everywhere

```yaml
host: gitlab.com
projects: []
```

Shows your open MRs in every project you have access to, with a Project column.

### Several projects

```yaml
projects:
  - my-group/android-app
  - my-group/ios-app
  - my-group/backend
```

All three are fetched on each refresh and merged into one list, most recently updated first. The
Project column tells them apart.

### Self-managed GitLab

```yaml
host: gitlab.example.com
projects:
  - platform/infrastructure
```

Log in with `glab auth login --hostname gitlab.example.com`, or set `GITLAB_TOKEN`.

### Only show review labels

```yaml
show_labels:
  - "^autoreview::"
```

Every other label is left out, so the Labels column only shows labels like `autoreview::clean` or
`autoreview::worth-fixing`.

### Show review labels, except while a review is running

```yaml
show_labels:
  - "^autoreview::"
hide_labels:
  - "^autoreview::running$"
```

`show_labels` keeps the `autoreview::` labels, then `hide_labels` removes `autoreview::running`.

### Hide a few scoped labels, keep the rest

```yaml
hide_labels:
  - "^platform::"
  - "^priority::"
  - "^team::"
```

### Match regardless of case

```yaml
show_labels:
  - "(?i)^bug$"
  - "(?i)^security"
```

Keeps `bug`, `Bug` and `BUG`, and any label starting with `security`, `Security` and so on.

### Refresh more or less often

```yaml
refresh_interval: 30s   # every 30 seconds
# refresh_interval: 5m  # every 5 minutes
```

### A settings file per directory

Put a `tglab.yaml` in a project checkout and run `tglab` from there: it's used instead of
`~/.config/tglab/config.yaml`.

```yaml
# ~/src/android-app/tglab.yaml
projects:
  - my-group/android-app
show_labels:
  - "^autoreview::"
```

### Several setups, picked by name

Keep separate files and choose one with `--config`, for example with shell aliases:

```sh
alias mrs-work='tglab --config ~/.config/tglab/work.yaml'
alias mrs-oss='tglab --config ~/.config/tglab/oss.yaml'
```

### Token from the environment

```sh
export GITLAB_TOKEN=glpat-xxxxxxxxxxxxxxxxxxxx
tglab
```

Useful when you don't use `glab` and don't want the token in a file.

## Troubleshooting

| Message | Cause | Fix |
|---|---|---|
| `parse …: yaml: unmarshal errors: … cannot unmarshal !!map into string` | A label pattern ending in `:` isn't quoted | Quote it: `- "^platform::"` |
| `…: show_labels: error parsing regexp: …` (or `hide_labels`) | An invalid regular expression | Fix the pattern; the message says which part is wrong |
| `no GitLab token: …` | None of the token sources is set | Run `glab auth login --hostname <host>`, set `GITLAB_TOKEN`, or add `token` |
| `GitLab API: 401 Unauthorized` | The token is wrong, expired or for another host | Create a new token with `read_api`, or log in with `glab` again |
| `…: project not found or no access` | A path in `projects` is misspelled, or you can't see it | Copy the path from the project's URL |
| `showing all MRs needs projects in the settings file` | `a` pressed with `projects: []` | Add at least one project |
| A setting seems to have no effect | Its name is misspelled, so it's ignored | Compare with `tglab --help` |
