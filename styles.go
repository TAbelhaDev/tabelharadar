package main

import (
	"github.com/TAbelhaDev/tabelhatuiui"
	"github.com/charmbracelet/lipgloss"
)

// App-specific styles on top of tabelatuiui's shared chrome (called as
// theme.Header/Footer/Panel/Title/Dim directly). Colors live in the theme
// resolved in theme.go (Catppuccin Mocha + the DMS accent).

// Semantic status styles follow the Catppuccin style guide — the same
// meanings the lib's Success/Warning/Error/Muted carry, kept as named
// helpers here because these words read better in the model's code.

func wipStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(colGreen).Bold(true)
}

func unpushedStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(colYellow).Bold(true)
}

func riskStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(colRed).Bold(true)
}

func staleStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(colOverlay1)
}

// Layout helpers come straight from the lib.

func padLines(s string, width int) string    { return tuiui.PadLines(s, width) }
func wrapText(s string, width int) string    { return tuiui.WrapText(s, width) }
func padToHeight(s string, lines int) string { return tuiui.PadToHeight(s, lines) }
