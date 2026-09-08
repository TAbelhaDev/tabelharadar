<div align="center">

# TAbelhaRadar

**A TUI that audits the git health of your local repositories** — WIP, unpushed
commits, repos with no remote, projects left alone for too long.

**English** · [Português](README.pt-BR.md)

[![Go Version](https://img.shields.io/github/go-mod/go-version/TAbelhaDev/tabelharadar?style=flat-square&logo=go&logoColor=white&color=00ADD8)](go.mod)
[![Built with Bubble Tea](https://img.shields.io/badge/built%20with-Bubble%20Tea-ff69b4?style=flat-square)](https://github.com/charmbracelet/bubbletea)
[![Powered by tabelhatuiui](https://img.shields.io/badge/theme-tabelhatuiui-d6b4f7?style=flat-square)](https://github.com/TAbelhaDev/tabelhatuiui)
[![License: AGPL-3.0](https://img.shields.io/badge/license-AGPL--3.0-blue?style=flat-square)](LICENSE)

[![ko-fi](https://ko-fi.com/img/githubbutton_sm.svg)](https://ko-fi.com/ianptkcs)

</div>

---

## What it is

A [Bubble Tea](https://github.com/charmbracelet/bubbletea) TUI that scans the
repositories and folders-of-repositories listed in the settings (by default only
`~/codigo/pessoal`) and audits the state of each project: what is in WIP, what has
unpushed commits, what has no remote configured (and therefore no backup off the
machine) and what has not been touched in a while — a disciplinary inspection of
your repositories.

There is no separate metadata file — the whole state is inferred from each
repository's own git, plus the first paragraph of prose from its
`README.md`/`PLANNING.md`/`ESCOPO.md`/`STACK.md`/`TODO.md`/`CLAUDE.md` (whichever
exists first), plus the bullets from that project's Claude Code memory index
(`~/.claude/projects/<slug>/memory/MEMORY.md`), when a session has run in there.

The theme and the shared chrome (header/footer/panels, ANSI-aware padding, the
`ipc ... --json` helpers) come from
[`tabelhatuiui`](https://github.com/TAbelhaDev/tabelhatuiui), the shared UI library
of my Bubble Tea TUIs.

## Contents

- [Installation](#installation)
- [Layout](#layout)
- [Usage](#usage)
- [IPC](#ipc)
- [Digest](#digest)
- [Configuration](#configuration)
- [License](#license)

## Installation

Requires Go 1.26+.

```bash
go install github.com/TAbelhaDev/tabelharadar@latest
```

That installs the binary as `tabelharadar` (matching the module name). To get the short `taradar`
name used throughout this README, build from source instead:

```bash
git clone https://github.com/TAbelhaDev/tabelharadar.git
cd tabelharadar
go build -o taradar .
```

## Layout

Three panels:

- **Projects** (left, 1/5 of the width) — names only, to move quickly through the
  whole list. The glyph before the name sums up the status: `○` no git, `●`
  uncommitted changes, `▲` unpushed commits, `✕` commits but no remote, `✓` clean
  and in sync.
- **status** (top right, 1/5 of the height) — dirty (uncommitted files), push
  (unpushed commits) and activity (how long since the last commit) for the
  selected project.
- **description** (below, 4/5 of the height) — everything else: path, last commit,
  warnings (no remote, stash), the description extracted from
  README/PLANNING/etc. and the bullets from that project's Claude Code memory.
  When the content does not fit, the title shows the visible range
  (`description (1–13/27)`) and it scrolls.

## Usage

```
taradar         # opens the TUI
taradar list    # plain-text dump, no TTY — useful for scripting
taradar digest  # turn recent activity into kanban updates (needs [digest] config)
```

Inside the TUI: `↑`/`↓` (or `j`/`k`) move through the project list,
`ctrl+h`/`ctrl+l` switch between the projects panel and the description one,
`j`/`k` (or `↑`/`↓`) scroll the description text while it is focused, `o`/`enter`
opens the selected project in `$EDITOR` (`nvim` by default), `r` rescans and `q`
quits.

## IPC

For scripts, or for an LLM to ask "what is left to do, where did I stop in each
project, what could be started" without opening the TUI, `taradar` exposes a
non-interactive `ipc` subcommand, in the same spirit as
`dcal ipc <method> --json`/`djobs ipc <method> --json`:

```bash
taradar ipc projects.list --json                  # every tracked project, with git status + description + next steps
taradar ipc projects.list dirty=true --json       # only those with uncommitted changes
taradar ipc projects.list name=tabelhacal --json   # one specific project
taradar ipc projects.list group=tabeladev --json   # only the projects in the "tabeladev" group
taradar ipc projects.next --json                  # the project taradar itself would prioritise (mid-flight > most recent)
taradar ipc groups.list --json                    # every configured group, with its project list
```

Beyond the git status fields (branch, dirty, ahead/behind, last commit), each
project in the JSON carries `description` (extracted from README/PLANNING/etc.),
`memory_notes` (the one-line hooks from the memory index) and `next_steps` — the
**entire** body (not just the truncated hook) of any memory of that project marked
`type: next-steps` in its own `~/.claude/projects/<slug>/memory/`, empty when the
project does not have one yet.

### Groups

`[[groups]]` names a subset of scanned projects, like a board in a kanban — a
project can belong to several groups, or none, and the mapping lives only in
`config.toml` (nothing is written back into any repo). `projects.list
group=X` filters to that group's members; a group name that matches nothing
configured returns an empty list plus a stderr warning, the same as any other
unmatched filter.

## Digest

`taradar digest` turns recent project activity into kanban updates — the
radar's "why". It gathers activity from the mapped projects, asks an LLM for a
structured plan and applies it to the kanban through its IPC. Nothing lives
inside the kanban: the board→projects mapping is the radar's own config.

Flow per run:

1. gather activity for each `[digest.boards]` project since the last run —
   git commits + current state, the Claude memory index, and (optional, off by
   default) recent opencode sessions via `opencode session list`;
2. read the current board state with `tkanban ipc boards.list`;
3. ask the configured LLM for a plan: `{"moves":[...],"updates":[...],"creates":[...]}`;
4. apply it with `tkanban ipc cards.move` / `cards.update` / `cards.create`
   (or just print it with `--dry-run` / `dry_run = true`).

```bash
taradar digest               # apply (requires [digest] with enabled = true)
taradar digest --dry-run     # print the plan, change nothing
taradar digest --install-timer  # systemd user timer from [digest].schedule
```

The cursor lives in `state_file` (`~/.local/state/tabelharadar/digest.json` by
default) and only advances on a real, non-dry run, so a preview never swallows
activity. Requires `tabelhakanban` ≥ v0.3.0 (the `ipc cards.update` method).

A minimal `[digest]` setup:

```toml
[digest]
enabled = true
wait_for_network = true   # wait for connectivity before running (default true)
network_timeout = "5m"

[digest.llm]
provider = "opencode"   # opencode | claude | deepseek | openai | anthropic

[digest.sources]
git = true
claude_memory = true

[[digest.boards]]
board = "geral"
projects = ["tabelhacal", "tabelhafin"]

[digest.schedule]       # used by `digest --install-timer`
on_calendar = "*-*-* 19:00:00"
```

`wait_for_network` exists because a `Persistent` timer fires the moment the
machine is back — usually before the network is up. The digest probes
github.com (up to `network_timeout`), and on timeout aborts cleanly without
advancing the cursor, so the next timer run retries. `--no-wait` skips it for
an interactive run.

Everything is a dial: `enabled` is the kill switch — with it off, `digest`
does nothing at all (no scan, no LLM, no write); `dry_run` (only relevant
once enabled) decides whether it writes, `[digest.llm]` how, `[digest.sources]`
what it sees, and `[[digest.boards]]` which boards feed which projects.

## Configuration

Everything lives in `~/.config/tabelharadar/config.toml` (overridable through
`TABELHARADAR_CONFIG`). The file is optional and partial: only the keys present
override anything, the rest stay on their defaults. `f5` reloads without
restarting.

```toml
roots = ["~/codigo/pessoal", "~/codigo/tabeladev"]
exclude = ["~/codigo/pessoal/spotdash"]

[[groups]]              # repeat for each group; optional, none by default
name = "tabeladev"
projects = ["tabelharadar", "tabelhakanban"]

[scanner]
description_files = ["README.md", "PLANNING.md", "ESCOPO.md", "STACK.md", "TODO.md", "CLAUDE.md"]
claude_projects_dir = "~/.claude/projects"

[layout]
sidebar_width_share = 1  # WIDTH ratio sidebar:right-column
right_width_share   = 4
stats_height_share  = 1  # HEIGHT ratio stats:description
desc_height_share   = 4

[general]
editor = "nvim"  # empty = use $EDITOR, then nvim

# All the digest keys are optional; see the Digest section for the full picture.
[digest]
enabled = false    # AI updating the kanban — opt-in
dry_run = false    # true = print the plan, write nothing
wait_for_network = true  # wait for connectivity before running (default true)
network_timeout = "5m"   # how long to wait (default 5m)

[digest.llm]
provider = "opencode"  # opencode | claude | deepseek | openai | anthropic
# model = "..."        # HTTP providers only (provider defaults when empty)
# base_url = ""        # override for OpenAI-compatible gateways
# api_key_env = ""     # DEEPSEEK_API_KEY / OPENAI_API_KEY / ANTHROPIC_API_KEY
# timeout = "120s"
# max_tokens = 0       # 0 = provider default
# temperature = 0.2

[digest.sources]
git               = true   # commits since last run + current state
claude_memory     = true   # MEMORY.md index + next-steps
opencode_sessions = false  # recent opencode sessions (read via the opencode CLI)
# since = "24h"            # window on the first run, before a state file exists

[[digest.boards]]          # repeat for each board
board = "geral"
projects = ["tabelhacal", "tabelhafin"]

[digest.schedule]          # used by `digest --install-timer`
on_calendar = "*-*-* 09:00:00"
persistent  = true
```

### Which repos to watch

`roots` accepts two kinds of path:

- a folder-of-repos: every subfolder with a `.git` becomes a row in the table
  (this is how `~/codigo/pessoal` works);
- a git repository directly: it enters on its own, as a single row (useful for a
  loose repo outside the usual folders).

`exclude` hides a specific path — whether a whole root or a child of a root listed
in `roots`. Order does not matter between the two lists.

With no file at all, it scans only `TABELHARADAR_ROOT` (or `~/codigo/pessoal`).

### Migrating from the old format

Before 0.3.0 the config was `~/.config/tabelharadar/config`, one entry per line with
`!` prefixing exclusions. **That file is still read** when no `config.toml` exists,
with a warning on the status bar. The translation is direct:

```
~/codigo/pessoal              →  roots   = ["~/codigo/pessoal"]
!~/codigo/pessoal/spotdash    →  exclude = ["~/codigo/pessoal/spotdash"]
```

Once `config.toml` exists, it takes over on its own — the two formats do not mix,
and the old file can be deleted.

### Other variables

- `TABELHARADAR_CONFIG` — path to `config.toml`, when it is not the default.
- `TABELHARADAR_ROOT` — the directory scanned when no config exists at all
  (`~/codigo/pessoal` by default).
- `TABELHARADAR_ACCENT` — a manual Catppuccin Mocha accent, used only when
  DankMaterialShell is not installed or configured (`mauve` by default).
- `TABELHARADAR_DMS_SETTINGS` — path to the DMS `settings.json`, when it is not the
  default.

## Development

```bash
go test ./...
```

## Changelog

See [CHANGELOG.md](CHANGELOG.md) for the version history.

## Support the project

- **Global**: [ko-fi.com/ianptkcs](https://ko-fi.com/ianptkcs)
- **Brazil (Pix)**: scan the QR below or copy the code

  <img src="pix-qr.png" alt="Pix QR" width="200" />

  <details><summary>Pix code (copy)</summary>

  ```
  00020126580014BR.GOV.BCB.PIX01365ad933b0-dcdc-4525-a736-0759902aeec65204000053039865802BR5925Ian Patrick da Costa Soar6009SAO PAULO62140510tQA85x6Dov63041FB6
  ```

  </details>

## License

[GNU AGPL-3.0](LICENSE) — free and open source. If you run a modified version of
this project, including as a network service, you also have to make the modified
source available under the same license.
