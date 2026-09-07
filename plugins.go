package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Plugin is the runtime representation of a discovered or configured plugin.
type Plugin struct {
	Name     string   `json:"name"`
	Bin      string   `json:"bin"`
	Version  string   `json:"version,omitempty"`
	Enabled  bool     `json:"enabled"`
	Methods  []string `json:"methods,omitempty"`
	NotFound bool     `json:"not_found,omitempty"`
}

// pluginMeta is the JSON output of `<bin> meta --json`.
type pluginMeta struct {
	Name    string   `json:"name"`
	Version string   `json:"version,omitempty"`
	Methods []string `json:"methods,omitempty"`
}

// discoverPlugins scans $PATH for taradar-* binaries and merges with the
// explicit plugin entries from config.toml. Discovered plugins default to
// enabled unless the config says otherwise.
func discoverPlugins() []Plugin {
	configMap := map[string]bool{}
	for _, pe := range settings.Plugins {
		configMap[pe.Name] = pe.Enabled
	}

	seen := map[string]bool{}
	var plugins []Plugin

	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			name := e.Name()
			if !strings.HasPrefix(name, "taradar-") {
				continue
			}
			suffix := strings.TrimPrefix(name, "taradar-")
			if suffix == "" || seen[suffix] {
				continue
			}
			seen[suffix] = true

			bin := filepath.Join(dir, name)
			p := Plugin{
				Name:    suffix,
				Bin:     bin,
				Enabled: true,
			}

			if enabled, ok := configMap[suffix]; ok {
				p.Enabled = enabled
			}

			if meta, err := pluginMetaQuery(bin); err == nil {
				p.Version = meta.Version
				p.Methods = meta.Methods
			}

			plugins = append(plugins, p)
		}
	}

	// Configured plugins not found in PATH.
	for _, pe := range settings.Plugins {
		if !seen[pe.Name] {
			plugins = append(plugins, Plugin{
				Name:     pe.Name,
				Bin:      "taradar-" + pe.Name,
				Enabled:  pe.Enabled,
				NotFound: true,
			})
		}
	}

	return plugins
}

// pluginMetaQuery runs `<bin> meta --json` and parses the output.
func pluginMetaQuery(bin string) (*pluginMeta, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, "meta", "--json")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var meta pluginMeta
	if err := json.Unmarshal(out, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

// pluginRun runs `<bin> run <method> [--json] [key=value...]`.
func pluginRun(bin, method string, args []string, jsonOutput bool) ([]byte, error) {
	cmdArgs := []string{"run", method}
	if jsonOutput {
		cmdArgs = append(cmdArgs, "--json")
	}
	cmdArgs = append(cmdArgs, args...)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, cmdArgs...)
	return cmd.Output()
}
