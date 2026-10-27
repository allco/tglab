# Notes for agents

tglab is a Go terminal app that keeps showing GitLab merge requests: by default the ones the user
opened, and with `a` every open MR in the configured projects. The [README](README.md) describes
what it does for users and [docs/configuration.md](docs/configuration.md) every setting. This file
covers what you need to keep working on it.

## Rules

- **This repository is public.** Never commit tokens, real company or project names, real MR
  titles, internal hostnames or work email addresses. Defaults, examples, tests and screenshots
  use made-up data (`my-group/my-project`, `acme/mobile-app`, `jane.doe`). The user's real
  settings live only in `~/.config/tglab/config.yaml`, outside the repo; `tglab.yaml` is
  gitignored because it may hold a token. Grep the diff for anything real before every commit.
- Commit as `Alexander Skvortsov <alexander.skvortsov@gmail.com>` (set as repo-local
  `user.email`), and end commit messages with `Co-Authored-By: Claude <noreply@anthropic.com>`.
- Commit and push only when asked; the user says "commit push".
- Don't poll GitHub Actions in a sleep loop. Check a run's state once and report it.
- Before committing: `gofmt -l .` (must print nothing), `go vet ./...`, `go test ./...`.
- Keep the README and `docs/configuration.md` in step with behaviour changes, and regenerate the
  screenshots after visible UI changes.

## Layout

| File | Contains |
|---|---|
| `main.go` | flags (`--config`, `--help`/`--usage`, `--version`), help text, startup |
| `config.go` | settings file lookup and defaults, label patterns, token lookup and glab renewal |
| `gitlab.go` | GraphQL queries, the `MR` type, the client with its 401 retry |
| `ui.go` | Bubble Tea model: keys, refresh loop, table rendering, status and colours |
| `splash.go` | start-up splash and the small corner logo |
| `*_test.go` | unit tests; `screenshot_test.go` renders the README screenshots |
| `scripts/build.sh`, `scripts/install.sh` | build with `git describe` as the version; install into `~/bin` (or `$INSTALL_DIR`) |
| `scripts/screenshots.sh` | turns the rendered screens into `docs/screenshots/*.png` |
| `.github/workflows/post-merge.yml` | "Post merge": gofmt, vet, test, build on each push to `main` |
| `.github/workflows/release.yml` | on a `vX.Y.Z` tag: test, cross-compile, publish a GitHub release |

## How it works

- **Stack:** Go 1.24.2+, Bubble Tea, lipgloss, bubbles (spinner only), yaml.v3. The table is
  rendered by hand with lipgloss, not `bubbles/table`, so rows can have coloured label pills and
  the selected row can grow to several lines.
- **Data:** GitLab GraphQL at `https://<host>/api/graphql`. Each refresh fetches every open MR in
  each project (`project.mergeRequests`, up to 10 pages of 100) into one cached list. "My MRs" is a
  filter on that list, so `a` switches views without a request. With no projects, it fetches the
  user's own MRs everywhere (`currentUser.authoredMergeRequests`) and `a` is disabled.
- **Threads** use `resolvableDiscussionsCount`/`resolvedDiscussionsCount`; loading discussions
  instead was about 8× slower.
- **Token:** `token` in the settings, then `GITLAB_TOKEN`, then `glab config get token --host`.
  glab's OAuth tokens expire after about 2 hours, so when the token came from glab the client
  re-reads it before each refresh and, on a 401, runs `glab api user --hostname <host>` (which
  makes glab renew it) and retries once.
- **Labels:** sorted case-insensitively. `show_labels` (whitelist, when non-empty) then
  `hide_labels` (blacklist) are regular expressions on the label name; they must be quoted in YAML
  because a trailing `:` makes a map. Hidden labels don't count as changes.
- **Rows:** one line each, with a `+N` overflow for labels that don't fit. The selected row
  expands to show every label, between `┄` dividers. `esc` clears the selection; up with nothing
  selected picks the last row. `●` marks MRs whose `Signature()` changed on the last refresh.
- **Status column:** `statusCell` picks one status in this order: `in train #N`, `conflicts`,
  `train after CI`, `in train`, `auto-merge`, then GitLab's `detailedMergeStatus` (`ready to
  merge`, `draft`, `checking`, `waiting for CI`, `needs approval`, `open threads`, `needs rebase`,
  `blocked`, or the raw status in lower case). `statusColor`: green ready, red conflicts/blocked,
  amber needs approval/threads/rebase, blue waiting for CI, purple train/auto-merge, grey the rest.
- **Splash:** shown for at least 1.2 s and until the first load ends; any key skips it. Afterwards
  a 3-line logo sits at the bottom right, beside the status and help lines, only when the rows
  leave two empty lines and the lines are short enough.

## Screenshots

`./scripts/screenshots.sh` runs `TestScreenshots` (skipped unless `TGLAB_SCREENSHOTS` is set),
which renders `View()` with the made-up MRs in `demoMRs()`. Then
[freeze](https://github.com/charmbracelet/freeze) v0.2.2 makes an SVG, and headless Google Chrome
makes the PNG. Chrome is needed because freeze's own PNG export draws `✔`/`✘` as boxes. Each
shot's height must leave room for the corner logo.

## Releases

Push an annotated tag such as `v1.2.0`; a tag with `-` (e.g. `v1.2.0-rc.1`) makes a pre-release.
The workflow builds darwin/linux/windows × amd64/arm64 with `-X main.version=<tag>` and attaches
bare binaries, archives with the README and example config, and `checksums.txt`. Released so far:
v1.0.0 and v1.1.0.

## Open items

- **Repository recreation (waiting on the user).** GitHub still serves a few unreachable commits
  from before the history was rewritten to a single initial commit. The user is to delete and
  recreate the GitHub repository (empty, public). Then push `main` and every tag, which rebuilds
  the releases, and check that the old commit URLs return 404. The hashes are in the user's
  private notes, not here.
- Offered, not yet asked for: a LICENSE file, and the status table above in the README's Columns
  section.
