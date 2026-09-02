package main

import (
	"path/filepath"

	"github.com/charmbracelet/bubbles/key"
	"github.com/ianptkcs/tabelatuiui"
)

// reg is tabelharadar's single source of truth for keybindings: defaults
// registered below, overrides persisted to ~/.config/tabelharadar/keybindings.json.
// Resolve() returns the effective binding, shared by dispatch, footer and
// help modal — a user rebind via the settings modal applies to all at once.
var reg = tuiui.NewKeyRegistry(filepath.Join(tuiui.ConfigDir(), "tabelharadar", "keybindings.json"))

func init() {
	reg.RegisterMany(
		tuiui.Action{ID: "quit", Help: "quit", Keys: []string{"q", "ctrl+c"}},
		tuiui.Action{ID: "help", Help: "keybindings", Keys: []string{"?"}},
		tuiui.Action{ID: "settings", Help: "rebind keys", Keys: []string{","}},
		tuiui.Action{ID: "refresh", Help: "rescan", Keys: []string{"r"}},
		tuiui.Action{ID: "reload", Help: "recarregar config", Keys: []string{"f5"}},
		tuiui.Action{ID: "open", Help: "abrir editor", Keys: []string{"o", "enter"}},
		tuiui.Action{ID: "focus-list", Help: "projetos", Keys: []string{"ctrl+h"}},
		tuiui.Action{ID: "focus-desc", Help: "descrição", Keys: []string{"ctrl+l"}},
		tuiui.Action{ID: "scroll", Help: "rolar descrição", Keys: []string{"j", "k", "up", "down"}, Label: "j/k"},
	)
}

// resolve is a short alias so Update reads like the old named keys.
func resolve(id string) key.Binding { return reg.Resolve(id) }
