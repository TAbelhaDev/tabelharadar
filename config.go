package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ianptkcs/tabelatuiui"
)

// rootEntry is one monitored path: a repo, a repo-group root, or (if Exclude)
// a path to hide from whatever root it's nested under.
type rootEntry struct {
	Path    string
	Exclude bool
}

// config is tabelaradar's settings schema, read from
// ~/.config/tabelaradar/config.toml. Every field falls back to defaultConfig
// when the file leaves it out.
type config struct {
	// Roots are the paths to monitor: a repo-group root whose child dirs each
	// become a project, or a single repo monitored on its own.
	Roots []string `toml:"roots"`
	// Exclude hides a path from whatever root it sits under. Order does not
	// matter — scanAll builds the exclusion set before walking.
	Exclude []string      `toml:"exclude"`
	Scanner scannerConfig `toml:"scanner"`
	Layout  layoutConfig  `toml:"layout"`
	General generalConfig `toml:"general"`
	// Digest gates the `digest` subcommand — the AI that turns gathered
	// activity into kanban updates. Everything about it is configurable and
	// it is off by default: no provider, no board mapping, no AI until the
	// user explicitly opts in.
	Digest digestConfig `toml:"digest"`
}

type scannerConfig struct {
	// DescriptionFiles is the priority order for a project's "what is this"
	// blurb. Whatever is listed first and exists wins; a *.md glob is the
	// fallback when none match.
	DescriptionFiles  []string `toml:"description_files"`
	ClaudeProjectsDir string   `toml:"claude_projects_dir"`
}

type layoutConfig struct {
	// Width ratio between the sidebar and the right column, side by side.
	SidebarWidthShare int `toml:"sidebar_width_share"`
	RightWidthShare   int `toml:"right_width_share"`
	// Height ratio between stats and description, stacked in the right column.
	StatsHeightShare int `toml:"stats_height_share"`
	DescHeightShare  int `toml:"desc_height_share"`
}

type generalConfig struct {
	// Editor overrides $EDITOR for the "open" key.
	Editor string `toml:"editor"`
}

// digestConfig is the whole `[digest]` section. Enabled defaults to false on
// purpose — the digest only runs at all once the user turns it on and maps
// at least one board.
type digestConfig struct {
	// Enabled is a full kill switch. With it off (the default), `digest` does
	// nothing at all — no project scan, no network wait, no LLM call, no
	// kanban write.
	Enabled bool `toml:"enabled"`
	// DryRun prints the planned kanban changes instead of applying them. It
	// does not bypass the LLM: the digest still asks it to propose moves, it
	// just refuses to write.
	DryRun bool `toml:"dry_run"`
	// StateFile is where the digest keeps its cursor — the last successful
	// run time, so the next run only gathers what happened since.
	StateFile string `toml:"state_file"`
	// KanbanBin is the tabelakanban binary the digest drives. Empty = look it
	// up on $PATH.
	KanbanBin string `toml:"kanban_bin"`
	// WaitForNetwork blocks the start of the run until a probe to github.com
	// succeeds. A Persistent timer fires as soon as the machine is back, which
	// is often before the network is up — this waits (up to NetworkTimeout)
	// and aborts cleanly if it never comes, so the cursor doesn't advance and
	// the next timer run retries. True by default; `--no-wait` overrides.
	WaitForNetwork bool           `toml:"wait_for_network"`
	NetworkTimeout duration       `toml:"network_timeout"`
	LLM            llmConfig      `toml:"llm"`
	Sources        digestSources  `toml:"sources"`
	Boards         []digestBoard  `toml:"boards"`
	Schedule       scheduleConfig `toml:"schedule"`
}

// llmConfig decides how and whether an AI runs. Provider picks the backend;
// the rest tune it. API keys are read from the environment (never the file).
type llmConfig struct {
	// Provider is one of: opencode, claude (local CLIs), deepseek, openai,
	// anthropic (HTTP APIs). Empty means no AI at all.
	Provider string `toml:"provider"`
	// Model overrides the provider default. Only the HTTP providers need it
	// (and even they fall back to a sane default when empty).
	Model string `toml:"model"`
	// BaseURL overrides the endpoint, for OpenAI-compatible gateways.
	BaseURL string `toml:"base_url"`
	// APIKeyEnv names the env var holding the key. Defaults to the
	// provider's own convention (DEEPSEEK_API_KEY, OPENAI_API_KEY,
	// ANTHROPIC_API_KEY) when empty.
	APIKeyEnv string `toml:"api_key_env"`
	// CLI overrides the binary for the local-CLI providers (opencode/claude).
	// Empty = look it up on $PATH.
	CLI string `toml:"cli"`
	// Timeout bounds a single LLM call.
	Timeout duration `toml:"timeout"`
	// MaxTokens caps the completion; 0 = provider default.
	MaxTokens int `toml:"max_tokens"`
	// Temperature for the completion; 0 = deterministic-ish, 1 = spicy.
	Temperature float64 `toml:"temperature"`
}

// digestSources picks which activity feeds the digest, per source. This is
// the "how and whether an AI runs" dial: a source off is simply never
// gathered, a source on is gathered and handed to the LLM.
type digestSources struct {
	// Git: commits since the last run plus the current repo state the scanner
	// already reads (dirty, ahead/behind, branch, last commit).
	Git bool `toml:"git"`
	// ClaudeMemory: the MEMORY.md index + next-steps body the scanner already
	// reads for each project.
	ClaudeMemory bool `toml:"claude_memory"`
	// OpencodeSessions: recent opencode session transcripts, read directly
	// from the opencode sqlite store (read-only). Off by default — the store
	// is big and its schema is opencode's, not ours.
	OpencodeSessions bool `toml:"opencode_sessions"`
	// Since is the fallback window on the very first run, when there is no
	// state file yet (e.g. "24h", "7d").
	Since string `toml:"since"`
}

// digestBoard maps one kanban board to the radar projects that feed it. Board
// matches the kanban board name (as `tabelakanban ipc boards.list` reports
// it); Projects are Project.Name values (the repo basenames the radar scans).
// The mapping is the radar's own — nothing new lives inside the kanban.
type digestBoard struct {
	Board    string   `toml:"board"`
	Projects []string `toml:"projects"`
}

// scheduleConfig feeds `digest --install-timer`: the systemd user timer that
// runs the digest on a schedule. Nothing is installed unless asked.
type scheduleConfig struct {
	OnCalendar string `toml:"on_calendar"`
	Persistent bool   `toml:"persistent"`
}

// duration wraps time.Duration so TOML can express it as "2s" instead of a
// raw nanosecond count.
type duration struct{ time.Duration }

func (d *duration) UnmarshalText(text []byte) error {
	parsed, err := time.ParseDuration(string(text))
	if err != nil {
		return err
	}
	d.Duration = parsed
	return nil
}

func defaultConfig() config {
	return config{
		Roots:   []string{tuiui.EnvOr("TABELARADAR_ROOT", filepath.Join(tuiui.HomeDir(), "codigo", "pessoal"))},
		Exclude: nil,
		Scanner: scannerConfig{
			DescriptionFiles:  []string{"README.md", "PLANNING.md", "ESCOPO.md", "STACK.md", "TODO.md", "CLAUDE.md"},
			ClaudeProjectsDir: filepath.Join(tuiui.HomeDir(), ".claude", "projects"),
		},
		Layout: layoutConfig{
			SidebarWidthShare: 1,
			RightWidthShare:   4,
			StatsHeightShare:  1,
			DescHeightShare:   4,
		},
		General: generalConfig{Editor: ""}, // empty = fall back to $EDITOR, then nvim
		Digest: digestConfig{
			StateFile:      filepath.Join(tuiui.HomeDir(), ".local", "state", "tabelaradar", "digest.json"),
			KanbanBin:      "tabelakanban",
			WaitForNetwork: true,
			NetworkTimeout: duration{5 * time.Minute},
			LLM: llmConfig{
				Provider:    "opencode", // the local CLI; the zero-extra-setup default
				Timeout:     duration{120 * time.Second},
				Temperature: 0.2,
			},
			Sources: digestSources{
				Git:              true,
				ClaudeMemory:     true,
				OpencodeSessions: false, // big store + opencode's own schema: opt-in
				Since:            "24h",
			},
			Schedule: scheduleConfig{OnCalendar: "*-*-* 19:00:00", Persistent: true},
		},
	}
}

// configPath is resolved lazily, not in a package-level var: an init-time var
// would freeze TABELARADAR_CONFIG/XDG_CONFIG_HOME before main (or a test)
// could set them.
func configPath() string {
	return tuiui.EnvOr("TABELARADAR_CONFIG", tuiui.ConfigPath("tabelaradar", "config.toml"))
}

// legacyConfigPath is the pre-TOML file: one path per line, "!" prefixing an
// exclusion. Still read when no config.toml exists yet, so an existing
// install keeps working untouched.
func legacyConfigPath() string {
	return tuiui.ConfigPath("tabelaradar", "config")
}

// settings is the normalized snapshot the app reads from.
var settings = defaultConfig()

// normalize clamps values the layout cannot survive. A share of zero would
// collapse a panel to nothing and divide by zero in the ratio math.
func normalize(c config) config {
	d := defaultConfig()
	if c.Layout.SidebarWidthShare < 1 {
		c.Layout.SidebarWidthShare = d.Layout.SidebarWidthShare
	}
	if c.Layout.RightWidthShare < 1 {
		c.Layout.RightWidthShare = d.Layout.RightWidthShare
	}
	if c.Layout.StatsHeightShare < 1 {
		c.Layout.StatsHeightShare = d.Layout.StatsHeightShare
	}
	if c.Layout.DescHeightShare < 1 {
		c.Layout.DescHeightShare = d.Layout.DescHeightShare
	}
	if len(c.Scanner.DescriptionFiles) == 0 {
		c.Scanner.DescriptionFiles = d.Scanner.DescriptionFiles
	}
	if c.Scanner.ClaudeProjectsDir == "" {
		c.Scanner.ClaudeProjectsDir = d.Scanner.ClaudeProjectsDir
	}
	c.Scanner.ClaudeProjectsDir = tuiui.ExpandHome(c.Scanner.ClaudeProjectsDir)
	if len(c.Roots) == 0 {
		c.Roots = d.Roots
	}
	if c.Digest.StateFile == "" {
		c.Digest.StateFile = d.Digest.StateFile
	}
	c.Digest.StateFile = tuiui.ExpandHome(c.Digest.StateFile)
	if c.Digest.LLM.Provider == "" {
		c.Digest.LLM.Provider = d.Digest.LLM.Provider
	}
	if c.Digest.LLM.APIKeyEnv == "" {
		switch c.Digest.LLM.Provider {
		case "deepseek":
			c.Digest.LLM.APIKeyEnv = "DEEPSEEK_API_KEY"
		case "openai":
			c.Digest.LLM.APIKeyEnv = "OPENAI_API_KEY"
		case "anthropic":
			c.Digest.LLM.APIKeyEnv = "ANTHROPIC_API_KEY"
		}
	}
	if c.Digest.LLM.Timeout.Duration <= 0 {
		c.Digest.LLM.Timeout = d.Digest.LLM.Timeout
	}
	if c.Digest.NetworkTimeout.Duration <= 0 {
		c.Digest.NetworkTimeout = d.Digest.NetworkTimeout
	}
	if c.Digest.Sources.Since == "" {
		c.Digest.Sources.Since = d.Digest.Sources.Since
	}
	if c.Digest.Schedule.OnCalendar == "" {
		c.Digest.Schedule.OnCalendar = d.Digest.Schedule.OnCalendar
	}
	return c
}

// refreshSettings re-reads config.toml from disk and returns a warning string
// (never an error) — a bad config file must not stop the scan, same contract
// the old line-based loader had.
// The Config is built per call rather than kept in a package var: both the
// path (TABELARADAR_CONFIG) and the defaults (TABELARADAR_ROOT) come from the
// environment, and a cached instance would freeze whatever they were on the
// first call. Nothing is lost — this app re-reads on every rescan anyway and
// never consults Reload's "changed" flag.
func refreshSettings() string {
	path := configPath()

	// No config.toml yet: fall back to the pre-TOML file if it's still there,
	// so an existing install keeps its roots after upgrading.
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if legacy, ok := loadLegacyConfig(); ok {
			settings = normalize(legacy)
			return fmt.Sprintf("lendo o config antigo %s — migre pra %s", legacyConfigPath(), path)
		}
	}

	cfg := tuiui.NewConfig(path, defaultConfig())
	err := cfg.Load()
	settings = normalize(cfg.Get())
	if err != nil {
		return fmt.Sprintf("erro lendo %s: %v", path, err)
	}
	return ""
}

// loadLegacyConfig reads the pre-TOML format: one path per line, "#" for
// comments, "!" prefixing an exclusion. Reports false when the file is absent
// or has no usable entries.
func loadLegacyConfig() (config, bool) {
	f, err := os.Open(legacyConfigPath())
	if err != nil {
		return config{}, false
	}
	defer f.Close()

	c := defaultConfig()
	c.Roots = nil
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if path := strings.TrimSpace(strings.TrimPrefix(line, "!")); strings.HasPrefix(line, "!") {
			c.Exclude = append(c.Exclude, path)
		} else {
			c.Roots = append(c.Roots, path)
		}
	}
	if scanner.Err() != nil || len(c.Roots) == 0 {
		return config{}, false
	}
	return c, true
}

// rootEntries flattens the configured roots and exclusions into the flat list
// scanAll walks, expanding "~" on the way.
func rootEntries() []rootEntry {
	entries := make([]rootEntry, 0, len(settings.Roots)+len(settings.Exclude))
	for _, p := range settings.Roots {
		entries = append(entries, rootEntry{Path: tuiui.ExpandHome(p)})
	}
	for _, p := range settings.Exclude {
		entries = append(entries, rootEntry{Path: tuiui.ExpandHome(p), Exclude: true})
	}
	return entries
}

// loadRootsConfig re-reads the config file and returns the roots to scan plus
// a warning string. It re-reads on every call by design: "r" (rescan) has
// always picked up an external edit to the roots without a restart.
func loadRootsConfig() ([]rootEntry, string) {
	warn := refreshSettings()
	return rootEntries(), warn
}
