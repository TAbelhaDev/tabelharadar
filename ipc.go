package main

import (
	"fmt"
	"os"
	"time"

	"github.com/TAbelhaDev/tabelhascaff/ipc"
)

// projectJSON is the wire format for the ipc subcommand — the same fields
// Project already carries, just with LastCommitTime rendered as RFC3339
// instead of Go's time.Time, and NextSteps/MemoryNotes as the actual
// content an LLM would want rather than table-formatted cells.
type projectJSON struct {
	Name string `json:"name"`
	Path string `json:"path"`

	IsGit     bool `json:"is_git"`
	NoCommits bool `json:"no_commits,omitempty"`

	Branch      string `json:"branch,omitempty"`
	DirtyCount  int    `json:"dirty_count"`
	StashCount  int    `json:"stash_count,omitempty"`
	HasRemote   bool   `json:"has_remote"`
	RemoteURL   string `json:"remote_url,omitempty"`
	HasUpstream bool   `json:"has_upstream,omitempty"`
	Ahead       int    `json:"ahead,omitempty"`
	Behind      int    `json:"behind,omitempty"`

	LastCommitTime string `json:"last_commit_time,omitempty"`
	LastCommitMsg  string `json:"last_commit_msg,omitempty"`

	Description    string `json:"description,omitempty"`
	DescriptionSrc string `json:"description_src,omitempty"`

	MemoryNotes []string `json:"memory_notes,omitempty"`
	// NextSteps is the full "o que falta fazer" text (see readMemory), not
	// just its one-line hook in MemoryNotes — empty if the project has no
	// memory file tagged type: next-steps yet.
	NextSteps string `json:"next_steps,omitempty"`
}

// groupJSON is the wire format for groups.list — the configured group
// verbatim, not which of those projects the scan actually found.
type groupJSON struct {
	Name     string   `json:"name"`
	Projects []string `json:"projects"`
}

func (p Project) toIPC() projectJSON {
	out := projectJSON{
		Name:           p.Name,
		Path:           p.Path,
		IsGit:          p.IsGit,
		NoCommits:      p.NoCommits,
		Branch:         p.Branch,
		DirtyCount:     p.DirtyCount,
		StashCount:     p.StashCount,
		HasRemote:      p.HasRemote,
		RemoteURL:      p.RemoteURL,
		HasUpstream:    p.HasUpstream,
		Ahead:          p.Ahead,
		Behind:         p.Behind,
		LastCommitMsg:  p.LastCommitMsg,
		Description:    p.Description,
		DescriptionSrc: p.DescriptionSrc,
		MemoryNotes:    p.MemoryNotes,
		NextSteps:      p.NextSteps,
	}
	if !p.LastCommitTime.IsZero() {
		out.LastCommitTime = p.LastCommitTime.Format(time.RFC3339)
	}
	return out
}

// runIPC implements `taradar ipc <method> [key=value...] --json`, the same
// scriptable-data-source convention as dcal/djobs (`<bin> ipc <method>
// --json`) — meant for an LLM (or any script) to ask "what's left to do,
// where did I stop, what could I pick up next" across every tracked repo
// without going through the TUI.
func runIPC(args []string) int {
	parsed, err := ipc.ParseIPCArgs(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "uso: taradar ipc <método> [key=value...] --json")
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	entries, cfgWarning := loadRootsConfig()
	if cfgWarning != "" {
		fmt.Fprintln(os.Stderr, "aviso:", cfgWarning)
	}

	// groups.list is config-only — it never needs the scan (scanAll shells
	// out to git per project), so it returns before paying for one.
	if parsed.Method == "groups.list" {
		return ipcGroupsList(settings.Groups)
	}

	projects, warnings := scanAll(entries)
	for _, w := range warnings {
		fmt.Fprintln(os.Stderr, "aviso:", w)
	}

	switch parsed.Method {
	case "projects.list":
		return ipcProjectsList(projects, settings.Groups, parsed.Filters)
	case "projects.next":
		return ipcProjectsNext(projects)
	case "plugins.list":
		return ipcPluginsList()
	case "plugins.toggle":
		return ipcPluginsToggle(parsed.Filters)
	default:
		fmt.Fprintf(os.Stderr, "método desconhecido: %q\n", parsed.Method)
		return 1
	}
}

// filterProjects applies the projects.list filters. The second return is a
// warning for stderr (empty when there's nothing to say).
func filterProjects(projects []Project, groups []groupConfig, filters map[string]string) ([]projectJSON, string) {
	var members map[string]bool
	var warning string
	if group, ok := filters["group"]; ok {
		m, found := groupMembers(groups, group)
		if !found {
			warning = fmt.Sprintf("grupo %q não existe no config", group)
		}
		members = m
	}

	out := make([]projectJSON, 0, len(projects))
	for _, p := range projects {
		if name, ok := filters["name"]; ok && p.Name != name {
			continue
		}
		if dirty, ok := filters["dirty"]; ok && (p.DirtyCount > 0) != (dirty == "true") {
			continue
		}
		if _, ok := filters["group"]; ok && !members[p.Name] {
			continue
		}
		out = append(out, p.toIPC())
	}
	return out, warning
}

func ipcProjectsList(projects []Project, groups []groupConfig, filters map[string]string) int {
	out, warning := filterProjects(projects, groups, filters)
	if warning != "" {
		fmt.Fprintln(os.Stderr, "aviso:", warning)
	}
	return ipc.WriteJSON(out)
}

// ipcGroupsList prints the configured groups verbatim — it reports the
// config's mapping, not which of those projects the scan actually found.
func ipcGroupsList(groups []groupConfig) int {
	out := make([]groupJSON, 0, len(groups))
	for _, g := range groups {
		projects := g.Projects
		if projects == nil {
			projects = []string{}
		}
		out = append(out, groupJSON{Name: g.Name, Projects: projects})
	}
	return ipc.WriteJSON(out)
}

// ipcProjectsNext returns the single project tabelharadar itself would put first —
// projects come back from scanAll already ordered mid-flight (dirty) work
// first, then most recently active, the same priority the TUI's sidebar
// shows top-to-bottom.
func ipcProjectsNext(projects []Project) int {
	if len(projects) == 0 {
		return ipc.WriteJSON(nil)
	}
	return ipc.WriteJSON(projects[0].toIPC())
}

// ipcPluginsList returns all discovered/configured plugins.
func ipcPluginsList() int {
	plugins := discoverPlugins()
	return ipc.WriteJSON(plugins)
}

// ipcPluginsToggle enables or disables a plugin. Filters: name=, enabled=true/false.
func ipcPluginsToggle(filters map[string]string) int {
	name := filters["name"]
	if name == "" {
		fmt.Fprintln(os.Stderr, "filtro name= é obrigatório")
		return 1
	}
	enabledStr := filters["enabled"]
	if enabledStr == "" {
		fmt.Fprintln(os.Stderr, "filtro enabled= é obrigatório")
		return 1
	}
	enabled := enabledStr == "true"

	// Find and update the plugin in settings.
	found := false
	for i, pe := range settings.Plugins {
		if pe.Name == name {
			settings.Plugins[i].Enabled = enabled
			found = true
			break
		}
	}
	if !found {
		settings.Plugins = append(settings.Plugins, pluginEntry{Name: name, Enabled: enabled})
	}

	return ipc.WriteJSON(map[string]any{
		"name":    name,
		"enabled": enabled,
	})
}
