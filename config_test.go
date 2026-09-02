package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// configDir points XDG_CONFIG_HOME at a temp dir and returns
// <tmp>/tabelharadar, where both the legacy "config" and the new "config.toml"
// live. It also resets the package-level cfg so each test loads fresh.
func configDir(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	t.Setenv("TABELHARADAR_CONFIG", "")
	t.Setenv("TABELHARADAR_ROOT", "")
	settings = defaultConfig()

	dir := filepath.Join(base, "tabelharadar")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// paths flattens rootEntries into two lists for easy comparison.
func paths(entries []rootEntry) (roots, excluded []string) {
	for _, e := range entries {
		if e.Exclude {
			excluded = append(excluded, e.Path)
		} else {
			roots = append(roots, e.Path)
		}
	}
	return roots, excluded
}

func TestLoadRootsConfigTOML(t *testing.T) {
	dir := configDir(t)
	write(t, filepath.Join(dir, "config.toml"), `
roots = ["/tmp/a", "/tmp/b"]
exclude = ["/tmp/a/skip"]
`)

	entries, warn := loadRootsConfig()
	if warn != "" {
		t.Fatalf("warning = %q, want none", warn)
	}
	roots, excluded := paths(entries)
	if len(roots) != 2 || roots[0] != "/tmp/a" || roots[1] != "/tmp/b" {
		t.Fatalf("roots = %v, want [/tmp/a /tmp/b]", roots)
	}
	if len(excluded) != 1 || excluded[0] != "/tmp/a/skip" {
		t.Fatalf("excluded = %v, want [/tmp/a/skip]", excluded)
	}
}

// The pre-TOML file must keep working after an upgrade: this is the only app
// of the batch with a config file that actually exists on users' disks.
func TestLoadRootsConfigFallsBackToLegacyFile(t *testing.T) {
	dir := configDir(t)
	write(t, filepath.Join(dir, "config"), `
# comentário
/tmp/pessoal
/tmp/tabelhadev
!/tmp/pessoal/sigapp
`)

	entries, warn := loadRootsConfig()
	if !strings.Contains(warn, "config antigo") {
		t.Fatalf("warning = %q, want a migration hint", warn)
	}
	roots, excluded := paths(entries)
	if len(roots) != 2 || roots[0] != "/tmp/pessoal" || roots[1] != "/tmp/tabelhadev" {
		t.Fatalf("roots = %v, want the 2 legacy roots", roots)
	}
	if len(excluded) != 1 || excluded[0] != "/tmp/pessoal/sigapp" {
		t.Fatalf("excluded = %v, want [/tmp/pessoal/sigapp]", excluded)
	}
}

// Once config.toml exists it wins outright — no merging of the two formats.
func TestTOMLTakesPrecedenceOverLegacy(t *testing.T) {
	dir := configDir(t)
	write(t, filepath.Join(dir, "config"), "/tmp/legacy\n")
	write(t, filepath.Join(dir, "config.toml"), `roots = ["/tmp/novo"]`+"\n")

	entries, warn := loadRootsConfig()
	if warn != "" {
		t.Fatalf("warning = %q, want none", warn)
	}
	roots, _ := paths(entries)
	if len(roots) != 1 || roots[0] != "/tmp/novo" {
		t.Fatalf("roots = %v, want [/tmp/novo]", roots)
	}
}

func TestLoadRootsConfigNoFilesUsesDefault(t *testing.T) {
	configDir(t)

	entries, warn := loadRootsConfig()
	if warn != "" {
		t.Fatalf("warning = %q, want none", warn)
	}
	roots, _ := paths(entries)
	if len(roots) != 1 {
		t.Fatalf("roots = %v, want a single default root", roots)
	}
	if !strings.HasSuffix(roots[0], filepath.Join("codigo", "pessoal")) {
		t.Fatalf("default root = %q, want ~/codigo/pessoal", roots[0])
	}
}

func TestMalformedTOMLWarnsAndKeepsScanning(t *testing.T) {
	dir := configDir(t)
	write(t, filepath.Join(dir, "config.toml"), "roots = [\nbroken")

	entries, warn := loadRootsConfig()
	if warn == "" {
		t.Fatal("warning = empty, want a parse error surfaced to the status bar")
	}
	// A bad config must not blank the scan — it degrades to defaults.
	if roots, _ := paths(entries); len(roots) == 0 {
		t.Fatal("roots = empty, want the defaults so the scan still runs")
	}
}

func TestOnlyKeysPresentOverrideDefaults(t *testing.T) {
	dir := configDir(t)
	write(t, filepath.Join(dir, "config.toml"), "[layout]\nsidebar_width_share = 2\n")

	if _, warn := loadRootsConfig(); warn != "" {
		t.Fatalf("warning = %q, want none", warn)
	}
	if settings.Layout.SidebarWidthShare != 2 {
		t.Fatalf("SidebarWidthShare = %d, want 2", settings.Layout.SidebarWidthShare)
	}
	if settings.Layout.RightWidthShare != 4 {
		t.Fatalf("RightWidthShare = %d, want the default 4", settings.Layout.RightWidthShare)
	}
	if len(settings.Scanner.DescriptionFiles) != 6 {
		t.Fatalf("DescriptionFiles = %v, want the 6 defaults", settings.Scanner.DescriptionFiles)
	}
}

func TestNormalizeRejectsZeroShares(t *testing.T) {
	// A zero share would divide by zero in the width/height ratio math.
	got := normalize(config{Layout: layoutConfig{SidebarWidthShare: 0, RightWidthShare: 0}})
	if got.Layout.SidebarWidthShare != 1 || got.Layout.RightWidthShare != 4 {
		t.Fatalf("layout = %+v, want the defaults restored", got.Layout)
	}
	if got.Layout.StatsHeightShare != 1 || got.Layout.DescHeightShare != 4 {
		t.Fatalf("height shares = %+v, want the defaults restored", got.Layout)
	}
}

func TestExpandHomeAppliedToRoots(t *testing.T) {
	dir := configDir(t)
	write(t, filepath.Join(dir, "config.toml"), `roots = ["~/codigo/x"]`+"\n")

	entries, _ := loadRootsConfig()
	roots, _ := paths(entries)
	if len(roots) != 1 || strings.HasPrefix(roots[0], "~") {
		t.Fatalf("roots = %v, want the ~ expanded", roots)
	}
}

func TestDigestConfigParsesSection(t *testing.T) {
	dir := configDir(t)
	write(t, filepath.Join(dir, "config.toml"), `
roots = ["/tmp/tabelhadev"]

[digest]
enabled = true
dry_run = true
kanban_bin = "/tmp/tkb"
state_file = "~/estado/digest.json"
wait_for_network = false
network_timeout = "2m"

[digest.llm]
provider = "deepseek"
model = "deepseek-chat"
api_key_env = "MY_KEY"
timeout = "30s"
max_tokens = 400
temperature = 0.5

[digest.sources]
git = false
claude_memory = false
opencode_sessions = true
since = "7d"

[[digest.boards]]
board = "geral"
projects = ["tabelhafin", "tabelhawebui"]

[[digest.boards]]
board = "wiv"
projects = ["blip-plugins"]

[digest.schedule]
on_calendar = "*-*-* 08:00:00"
persistent = false
`)
	if _, warn := loadRootsConfig(); warn != "" {
		t.Fatalf("warning = %q, want none", warn)
	}

	d := settings.Digest
	if !d.Enabled || !d.DryRun {
		t.Fatalf("enabled/dry_run = %v/%v, want true/true", d.Enabled, d.DryRun)
	}
	if d.KanbanBin != "/tmp/tkb" {
		t.Fatalf("kanban_bin = %q, want /tmp/tkb", d.KanbanBin)
	}
	if strings.HasPrefix(d.StateFile, "~") {
		t.Fatalf("state_file not expanded: %q", d.StateFile)
	}
	if d.WaitForNetwork || d.NetworkTimeout.Duration != 2*time.Minute {
		t.Fatalf("wait_for_network/network_timeout = %v/%v, want false/2m", d.WaitForNetwork, d.NetworkTimeout.Duration)
	}
	if d.LLM.Provider != "deepseek" || d.LLM.Model != "deepseek-chat" || d.LLM.APIKeyEnv != "MY_KEY" {
		t.Fatalf("llm = %+v, want deepseek/deepseek-chat/MY_KEY", d.LLM)
	}
	if d.LLM.Timeout.Duration != 30*time.Second {
		t.Fatalf("llm timeout = %v, want 30s", d.LLM.Timeout.Duration)
	}
	if d.LLM.MaxTokens != 400 || d.LLM.Temperature != 0.5 {
		t.Fatalf("llm max_tokens/temperature = %d/%v", d.LLM.MaxTokens, d.LLM.Temperature)
	}
	if d.Sources.Git || d.Sources.ClaudeMemory || !d.Sources.OpencodeSessions || d.Sources.Since != "7d" {
		t.Fatalf("sources = %+v, want only opencode_sessions + since 7d", d.Sources)
	}
	if len(d.Boards) != 2 || d.Boards[0].Board != "geral" || len(d.Boards[0].Projects) != 2 {
		t.Fatalf("boards = %+v, want geral com 2 projetos", d.Boards)
	}
	if d.Boards[1].Board != "wiv" || len(d.Boards[1].Projects) != 1 {
		t.Fatalf("boards[1] = %+v, want wiv com 1 projeto", d.Boards[1])
	}
	if d.Schedule.OnCalendar != "*-*-* 08:00:00" || d.Schedule.Persistent {
		t.Fatalf("schedule = %+v, want 08:00 + non-persistent", d.Schedule)
	}
}

func TestDigestConfigDefaults(t *testing.T) {
	configDir(t)
	if _, warn := loadRootsConfig(); warn != "" {
		t.Fatalf("warning = %q, want none", warn)
	}
	d := settings.Digest
	if d.Enabled || d.DryRun {
		t.Fatalf("digest should default to disabled, got %+v", d)
	}
	if d.LLM.Provider != "opencode" {
		t.Fatalf("default provider = %q, want opencode", d.LLM.Provider)
	}
	if !d.Sources.Git || !d.Sources.ClaudeMemory || d.Sources.OpencodeSessions {
		t.Fatalf("default sources = %+v, want git+memory on, sessions off", d.Sources)
	}
	if d.LLM.APIKeyEnv != "" {
		t.Fatalf("default api_key_env = %q, want empty (fallback per provider)", d.LLM.APIKeyEnv)
	}
	if !d.WaitForNetwork || d.NetworkTimeout.Duration != 5*time.Minute {
		t.Fatalf("default wait/network = %v/%v, want true/5m", d.WaitForNetwork, d.NetworkTimeout.Duration)
	}
	if d.Schedule.OnCalendar != "*-*-* 19:00:00" || !d.Schedule.Persistent {
		t.Fatalf("default schedule = %+v, want end-of-day persistent", d.Schedule)
	}
}

func TestDigestAPIKeyEnvFallsBackPerProvider(t *testing.T) {
	dir := configDir(t)
	write(t, filepath.Join(dir, "config.toml"), `[digest.llm]
provider = "anthropic"
`)
	if _, warn := loadRootsConfig(); warn != "" {
		t.Fatalf("warning = %q, want none", warn)
	}
	if settings.Digest.LLM.APIKeyEnv != "ANTHROPIC_API_KEY" {
		t.Fatalf("api_key_env = %q, want ANTHROPIC_API_KEY", settings.Digest.LLM.APIKeyEnv)
	}
}
