package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/TAbelhaDev/tabelhatuiui"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// panelFocus selects which of the (up to three) interactive panels — the
// groups sidebar, the projects sidebar, or the description panel — currently
// receives key input. The constants are declared in spatial left-to-right
// order: vim-style ctrl+h/l move focus one panel over, in that order, rather
// than binding to a fixed panel — see focusLeft/focusRight. The stats panel
// (top-right) is display-only and never a focus target.
type panelFocus int

const (
	focusGroups panelFocus = iota
	focusList
	focusDescription
)

// Vertical overhead, in lines: header + footer + each panel's own
// border/title, plus the sidebar's own bubbles/table header row.
const (
	headerLines        = 1
	footerLines        = 1
	sidebarBoxOverhead = 2 + 1 + 1 // border + title + bubbles/table header row
	statsBoxOverhead   = 2 + 1     // border + title
	descBoxOverhead    = 2 + 1     // border + title
	minVisibleRows     = 3
	minStatsLines      = 3
	minDescLines       = 4
	panelGap           = 1
	minSidebarWidth    = 14
	minRightWidth      = 30
	// The groups sidebar's inner width: 10 mirrors the old "Grupo" column
	// this replaces, 24 keeps a long group name from eating too much of the
	// row when the groups pane's width share is generous.
	minGroupsWidth = 10
	maxGroupsWidth = 24
)

// The sidebar:right-column width ratio and the stats:description height ratio
// now live in config.toml ([layout]); normalize keeps every share >= 1 so the
// divisions below can't hit zero.

// groupEntry is one row of the groups sidebar: either a real configured
// group by Name, or the pseudo-group "Todos" (All) that shows every project
// regardless of group membership.
type groupEntry struct {
	Name string
	All  bool
}

type appModel struct {
	projects []Project
	tbl      table.Model
	// gtbl is the groups sidebar — a second, one-column table listing
	// groupEntries. Only rendered/focusable when showGroupsPane is true.
	gtbl  table.Model
	focus panelFocus
	// detailScroll is the first visible line of the current project's
	// description panel, adjusted by j/k while focus is on the description.
	detailScroll int

	groupEntries []groupEntry
	// selectedGroup/selectedGroupAll track the groups sidebar's selection
	// across rescans, since gtbl's own rows get rebuilt each time.
	selectedGroup    string
	selectedGroupAll bool
	// visible is exactly the projects rendered in tbl, in row order — the
	// only thing current() ever indexes into, so a displayed row always
	// matches the project it points at even after group filtering reorders
	// or drops entries relative to m.projects.
	visible []Project
	// selectedPath tracks the projects sidebar's selection by Project.Path
	// (stable across rescans/filters; Name can repeat across roots).
	selectedPath string
	// showGroupsPane is false whenever no [[groups]] are configured — then
	// the layout/focus/view all fall back to exactly today's two-panel look.
	showGroupsPane bool

	width  int
	height int

	groupsInnerWidth  int
	sidebarInnerWidth int
	rightInnerWidth   int
	statsLines        int
	descMaxLines      int

	status string

	// helpModal is the "?" overlay listing every keybinding; settingsModal is
	// the "," overlay that lets the user rebind them. Both read from reg.
	helpModal     *tuiui.HelpModal
	settingsModal *tuiui.SettingsModal
}

func newModel() appModel {
	_ = reg.Load()
	m := appModel{
		helpModal: tuiui.NewHelpModal(tuiui.HelpSection{
			Title:      "Atalhos",
			BindingsFn: reg.Bindings,
		}),
		settingsModal: tuiui.NewSettingsModal(reg),
		focus:         focusList,
	}
	m.tbl = table.New(table.WithFocused(true))
	m.gtbl = table.New(table.WithFocused(true))
	// Placeholder single columns: bubbles/table's SetRows renders eagerly and
	// panics (index out of range) if called before SetColumns ever ran. The
	// real widths come from layout() once the first WindowSizeMsg arrives;
	// rescan() below calls SetRows before that happens.
	m.tbl.SetColumns([]table.Column{{Title: "Projeto", Width: minSidebarWidth - 2}})
	m.gtbl.SetColumns([]table.Column{{Title: "Grupo", Width: minGroupsWidth - 2}})
	m.applyStyles()
	m.rescan()
	return m
}

func (m *appModel) rescan() {
	entries, cfgWarning := loadRootsConfig()
	projects, warnings := scanAll(entries)
	if cfgWarning != "" {
		warnings = append([]string{cfgWarning}, warnings...)
	}

	m.projects = projects
	m.refreshGroups()
	m.applyGroupFilter()

	m.status = fmt.Sprintf("%d projetos", len(projects))
	if m.showGroupsPane && !settings.General.ShowAllGroup {
		if outOfGroup := countOutOfGroup(projects, settings.Groups); outOfGroup > 0 {
			warnings = append(warnings, fmt.Sprintf("%d fora de grupo (show_all_group)", outOfGroup))
		}
	}
	if len(warnings) > 0 {
		m.status += " — " + strings.Join(warnings, "; ")
	}
}

func (m *appModel) applyStyles() {
	styles := table.DefaultStyles()
	styles.Header = styles.Header.
		Foreground(colSubtext0).
		Background(colMantle).
		Bold(true)
	styles.Selected = styles.Selected.
		Foreground(colBase).
		Background(colPrimary).
		Bold(true)
	// Cell intentionally has no Background: bubbles/table renders each cell
	// individually and only wraps the whole row in Selected afterwards, so a
	// background baked into every cell's own ANSI codes would nest inside —
	// and win over — Selected's background on the row, hiding the highlight
	// entirely. Leaving Cell transparent lets the panel's own background
	// (colBase) show through for both normal and selected rows.
	m.tbl.SetStyles(styles)
	m.gtbl.SetStyles(styles)
}

// refreshGroups rebuilds the groups sidebar's entries from settings.Groups —
// the pseudo-group "Todos" first when show_all_group is on, then every
// configured group name in config order. It tries to keep the cursor on the
// same logical selection (by name/All) across a rescan, falling back to the
// first entry when that selection no longer exists (e.g. a group was
// removed from config.toml).
func (m *appModel) refreshGroups() {
	m.showGroupsPane = len(settings.Groups) > 0
	if !m.showGroupsPane {
		m.groupEntries = nil
		// The groups panel just disappeared (e.g. the last [[groups]] entry
		// was removed from config.toml and "r"/F5 re-read it) — if focus was
		// on it, nothing else would ever move focus off a panel that no
		// longer renders, leaving both key input and the focus border stuck
		// on a panel nobody can see.
		if m.focus == focusGroups {
			m.focus = focusList
		}
		return
	}

	entries := make([]groupEntry, 0, len(settings.Groups)+1)
	if settings.General.ShowAllGroup {
		entries = append(entries, groupEntry{Name: "Todos", All: true})
	}
	for _, name := range groupNames(settings.Groups) {
		entries = append(entries, groupEntry{Name: name})
	}
	m.groupEntries = entries

	idx := 0
	for i, e := range entries {
		if e.All == m.selectedGroupAll && e.Name == m.selectedGroup {
			idx = i
			break
		}
	}

	rows := make([]table.Row, len(entries))
	for i, e := range entries {
		rows[i] = table.Row{e.Name}
	}
	m.gtbl.SetRows(rows)
	m.gtbl.SetCursor(idx)
	m.selectedGroup = entries[idx].Name
	m.selectedGroupAll = entries[idx].All
}

// applyGroupFilter rebuilds m.visible — the projects sidebar's rows, filtered
// by whatever group is currently selected — and restores the cursor to
// selectedPath's row when it's still present. This is the sole place m.visible
// is written, which is what keeps current()'s indexing honest.
func (m *appModel) applyGroupFilter() {
	m.visible = filterByGroup(m.projects, settings.Groups, m.currentGroupEntry())
	m.tbl.SetRows(projectRows(m.visible))

	idx := 0
	for i, p := range m.visible {
		if p.Path == m.selectedPath {
			idx = i
			break
		}
	}
	m.tbl.SetCursor(idx)

	prevPath := m.selectedPath
	if idx < len(m.visible) {
		m.selectedPath = m.visible[idx].Path
	} else {
		m.selectedPath = ""
	}
	if m.selectedPath != prevPath {
		m.detailScroll = 0
	}
}

// currentGroupEntry is the groups sidebar's current selection, expressed as
// the groupEntry filterByGroup expects. With no groups pane at all, it's
// equivalent to "Todos" — filterByGroup already short-circuits on that.
func (m appModel) currentGroupEntry() groupEntry {
	if !m.showGroupsPane {
		return groupEntry{All: true}
	}
	return groupEntry{Name: m.selectedGroup, All: m.selectedGroupAll}
}

// countOutOfGroup counts scanned projects that belong to no configured group
// at all — the edge case where turning on [[groups]] without show_all_group
// silently drops most of the sidebar.
func countOutOfGroup(projects []Project, groups []groupConfig) int {
	if len(groups) == 0 {
		return 0
	}
	grouped := make(map[string]bool)
	for _, g := range groups {
		for _, name := range g.Projects {
			grouped[name] = true
		}
	}
	count := 0
	for _, p := range projects {
		if !grouped[p.Name] {
			count++
		}
	}
	return count
}

func (m appModel) Init() tea.Cmd { return nil }

func (m appModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if sizeMsg, ok := msg.(tea.WindowSizeMsg); ok {
		m.width, m.height = sizeMsg.Width, sizeMsg.Height
		m.helpModal.SetSize(sizeMsg.Width, sizeMsg.Height)
		m.settingsModal.SetSize(sizeMsg.Width, sizeMsg.Height)
		m.layout()
		return m, nil
	}
	if _, ok := msg.(editorFinishedMsg); ok {
		m.rescan()
		m.layout()
		return m, nil
	}

	// The settings/help modals swallow all keys while open — the app must
	// not act on them (so "q" closes the modal instead of quitting, etc.).
	if m.settingsModal.Update(msg) {
		return m, nil
	}
	if m.helpModal.Update(msg) {
		return m, nil
	}

	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m.forwardToTable(msg)
	}

	switch {
	case key.Matches(keyMsg, resolve("quit")):
		return m, tea.Quit
	case key.Matches(keyMsg, resolve("help")):
		m.helpModal.Toggle()
		return m, nil
	case key.Matches(keyMsg, resolve("settings")):
		m.settingsModal.Toggle()
		return m, nil
	case key.Matches(keyMsg, resolve("refresh")):
		m.rescan()
		m.layout()
		return m, nil
	case key.Matches(keyMsg, resolve("reload")):
		// Config-file-first: external edits to keybindings.json and config.toml
		// take effect here, without restarting. rescan re-reads config.toml on
		// its own (and reports a bad one through m.status), so this only has to
		// add the keybindings half.
		changed, err := reg.Reload()
		m.rescan()
		m.layout()
		switch {
		case err != nil:
			m.status = "keybindings: " + err.Error()
		case changed:
			m.status += " — keybindings recarregadas"
		}
		return m, nil
	case key.Matches(keyMsg, resolve("open")):
		if p := m.current(); p != nil {
			return m, openEditor(p.Path)
		}
		return m, nil
	// vim-style pane navigation: ctrl+h/ctrl+l move focus one panel over, in
	// spatial left-to-right order (groups, list, description) — not bound to
	// a fixed panel, since which panels exist depends on showGroupsPane.
	case key.Matches(keyMsg, resolve("focus-list")):
		m.focusLeft()
		return m, nil
	case key.Matches(keyMsg, resolve("focus-desc")):
		m.focusRight()
		return m, nil
	}

	if m.focus == focusDescription {
		switch keyMsg.String() {
		case "j", "down":
			if m.detailScroll < m.maxDetailScroll() {
				m.detailScroll++
			}
		case "k", "up":
			if m.detailScroll > 0 {
				m.detailScroll--
			}
		}
		return m, nil
	}

	if m.focus == focusGroups {
		return m.forwardToGroups(msg)
	}

	return m.forwardToTable(msg)
}

// focusLeft/focusRight move focus one panel over, in left-to-right spatial
// order (groups, list, description), skipping the groups panel when
// showGroupsPane is false. Neither wraps at either end.
func (m *appModel) focusLeft() {
	switch m.focus {
	case focusDescription:
		m.focus = focusList
	case focusList:
		if m.showGroupsPane {
			m.focus = focusGroups
		}
	}
}

func (m *appModel) focusRight() {
	switch m.focus {
	case focusGroups:
		m.focus = focusList
	case focusList:
		m.focus = focusDescription
	}
}

// forwardToTable forwards to the sidebar's table, resetting detailScroll and
// updating selectedPath if that moved the cursor to a different project — the
// old scroll offset otherwise makes no sense against new description text.
func (m appModel) forwardToTable(msg tea.Msg) (tea.Model, tea.Cmd) {
	prevIdx := m.tbl.Cursor()
	var cmd tea.Cmd
	m.tbl, cmd = m.tbl.Update(msg)
	if m.tbl.Cursor() != prevIdx {
		m.detailScroll = 0
		if p := m.current(); p != nil {
			m.selectedPath = p.Path
		} else {
			m.selectedPath = ""
		}
	}
	return m, cmd
}

// forwardToGroups forwards to the groups sidebar's table. A cursor move here
// filters the projects sidebar live, without Enter — moving the group cursor
// and updating the visible project list are the same user action.
func (m appModel) forwardToGroups(msg tea.Msg) (tea.Model, tea.Cmd) {
	prevIdx := m.gtbl.Cursor()
	var cmd tea.Cmd
	m.gtbl, cmd = m.gtbl.Update(msg)
	if idx := m.gtbl.Cursor(); idx != prevIdx {
		if idx >= 0 && idx < len(m.groupEntries) {
			e := m.groupEntries[idx]
			m.selectedGroup = e.Name
			m.selectedGroupAll = e.All
		}
		m.applyGroupFilter()
		m.detailScroll = 0
	}
	return m, cmd
}

// current indexes m.visible — exactly what generated the sidebar's rows — so
// row N always corresponds to m.visible[N], regardless of how the group
// filter reordered or dropped projects relative to m.projects.
func (m appModel) current() *Project {
	row := m.tbl.Cursor()
	if row < 0 || row >= len(m.visible) {
		return nil
	}
	return &m.visible[row]
}

type editorFinishedMsg struct{}

// openEditor suspends the TUI to run the editor against the selected
// project's root, the same "jump straight into it" shortcut lazygit-style
// tools give you once you've spotted what needs attention. Resolution order:
// config.toml's [general].editor, then $EDITOR, then nvim.
func openEditor(path string) tea.Cmd {
	editor := settings.General.Editor
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "nvim"
	}
	cmd := exec.Command(editor, path)
	return tea.ExecProcess(cmd, func(err error) tea.Msg { return editorFinishedMsg{} })
}

// layout recomputes the sidebar/stats/description panel widths and heights so
// the whole layout always fits exactly within m.height — the stats and
// description panels get a fixed line budget (statsLines/descMaxLines) that
// their content is padded or clipped to, instead of growing with whatever
// text happens to be in them. A panel emitting more lines than its budget
// is exactly what used to push the sidebar off the top of the screen for
// projects with a long description or many memory notes.
func (m *appModel) layout() {
	if m.width == 0 || m.height == 0 {
		return
	}

	G, S, R := settings.Layout.GroupsWidthShare, settings.Layout.SidebarWidthShare, settings.Layout.RightWidthShare

	var groupsBoxWidth, sidebarBoxWidth, rightBoxWidth int
	if m.showGroupsPane {
		totalRowWidth := m.width - 2*panelGap
		minRow := (minGroupsWidth + 4) + (minSidebarWidth + 4) + (minRightWidth + 4)
		if totalRowWidth < minRow {
			totalRowWidth = minRow
		}
		groupsBoxWidth = totalRowWidth * G / (G + S + R)
		if groupsBoxWidth < minGroupsWidth+4 {
			groupsBoxWidth = minGroupsWidth + 4
		}
		if groupsBoxWidth > maxGroupsWidth+4 {
			groupsBoxWidth = maxGroupsWidth + 4
		}
		remaining := totalRowWidth - groupsBoxWidth
		sidebarBoxWidth = remaining * S / (S + R)
		rightBoxWidth = remaining - sidebarBoxWidth
	} else {
		totalRowWidth := m.width - panelGap
		if minRow := (minSidebarWidth + 4) + (minRightWidth + 4); totalRowWidth < minRow {
			totalRowWidth = minRow
		}
		sidebarBoxWidth = totalRowWidth * S / (S + R)
		rightBoxWidth = totalRowWidth - sidebarBoxWidth
	}

	m.groupsInnerWidth = groupsBoxWidth - 4
	if m.groupsInnerWidth < minGroupsWidth {
		m.groupsInnerWidth = minGroupsWidth
	}
	m.sidebarInnerWidth = sidebarBoxWidth - 4
	if m.sidebarInnerWidth < minSidebarWidth {
		m.sidebarInnerWidth = minSidebarWidth
	}
	m.rightInnerWidth = rightBoxWidth - 4
	if m.rightInnerWidth < minRightWidth {
		m.rightInnerWidth = minRightWidth
	}

	// -2: bubbles/table's default Header/Cell styles each carry their own
	// Padding(0,1), added on top of the column's Width — the exact off-by-2
	// this project already hit once before with a 7-column table.
	if m.showGroupsPane {
		groupColWidth := m.groupsInnerWidth - 2
		if groupColWidth < 1 {
			groupColWidth = 1
		}
		m.gtbl.SetColumns([]table.Column{{Title: "Grupo", Width: groupColWidth}})
		m.gtbl.SetWidth(m.groupsInnerWidth)
	}

	nameColWidth := m.sidebarInnerWidth - 2
	if nameColWidth < 1 {
		nameColWidth = 1
	}
	m.tbl.SetColumns([]table.Column{{Title: "Projeto", Width: nameColWidth}})
	m.tbl.SetWidth(m.sidebarInnerWidth)

	bodyHeight := m.height - headerLines - footerLines
	minBody := statsBoxOverhead + minStatsLines + descBoxOverhead + minDescLines
	if minSidebarBody := sidebarBoxOverhead + minVisibleRows; minSidebarBody > minBody {
		minBody = minSidebarBody
	}
	if bodyHeight < minBody {
		bodyHeight = minBody
	}

	statsBoxHeight := bodyHeight * settings.Layout.StatsHeightShare / (settings.Layout.StatsHeightShare + settings.Layout.DescHeightShare)
	if statsBoxHeight < statsBoxOverhead+minStatsLines {
		statsBoxHeight = statsBoxOverhead + minStatsLines
	}
	descBoxHeight := bodyHeight - statsBoxHeight
	if descBoxHeight < descBoxOverhead+minDescLines {
		descBoxHeight = descBoxOverhead + minDescLines
	}

	m.statsLines = statsBoxHeight - statsBoxOverhead
	m.descMaxLines = descBoxHeight - descBoxOverhead

	// The sidebar (and, when shown, the groups panel) spans both right-column
	// boxes stacked together, so its row budget must match their combined
	// (post-clamp) height exactly or the borders won't line up at the bottom.
	sidebarRowsHeight := (statsBoxHeight + descBoxHeight) - sidebarBoxOverhead
	if sidebarRowsHeight < minVisibleRows {
		sidebarRowsHeight = minVisibleRows
	}
	m.tbl.SetHeight(sidebarRowsHeight)
	if m.showGroupsPane {
		m.gtbl.SetHeight(sidebarRowsHeight)
	}
}

// projectRows renders the projects sidebar's single column: a status glyph
// plus the project name. Order is whatever the caller passed in — always
// m.visible, which preserves scanAll's own dirty-first/most-recent priority.
func projectRows(projects []Project) []table.Row {
	rows := make([]table.Row, len(projects))
	for i, p := range projects {
		rows[i] = table.Row{fmt.Sprintf("%s %s", statusGlyph(p), p.Name)}
	}
	return rows
}

// filterByGroup returns the subset of projects belonging to group e,
// preserving projects' original order. e.All (the pseudo-group "Todos"), or
// no groups configured at all, returns projects unchanged. A project in
// several [[groups]] simply shows up under each — group membership here is
// nothing more than "is this project's name in that group's Projects list",
// so there's no map to keep in sync, and no "first group wins" ambiguity.
func filterByGroup(projects []Project, groups []groupConfig, e groupEntry) []Project {
	if e.All || len(groups) == 0 {
		return projects
	}
	members, _ := groupMembers(groups, e.Name)
	out := make([]Project, 0, len(projects))
	for _, p := range projects {
		if members[p.Name] {
			out = append(out, p)
		}
	}
	return out
}

func statusGlyph(p Project) string {
	switch {
	case !p.IsGit:
		return "○"
	case p.DirtyCount > 0:
		return "●"
	case p.Ahead > 0:
		return "▲"
	case p.IsGit && !p.HasRemote:
		return "✕"
	default:
		return "✓"
	}
}

func dirtyCell(p Project) string {
	if !p.IsGit || p.DirtyCount == 0 {
		return "-"
	}
	return fmt.Sprintf("%d", p.DirtyCount)
}

func pushCell(p Project) string {
	if !p.IsGit || !p.HasUpstream {
		return "-"
	}
	if p.Ahead == 0 {
		return "-"
	}
	return fmt.Sprintf("+%d", p.Ahead)
}

func remoteCell(p Project) string {
	if !p.IsGit {
		return "n/a"
	}
	if p.HasRemote {
		return "sim"
	}
	return "não"
}

func humanizeAgo(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := time.Since(t)
	switch {
	case d < time.Hour:
		return "agora"
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh atrás", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd atrás", int(d.Hours()/24))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dm atrás", int(d.Hours()/24/30))
	default:
		return fmt.Sprintf("%dy atrás", int(d.Hours()/24/365))
	}
}

func (m appModel) View() string {
	if m.width == 0 {
		return ""
	}

	header := theme.Header(m.width).Render("TAbelhaRadar — comissão central de inspeção disciplinar dos seus projetos")

	footer := tuiui.NewFooter(reg.Bindings()...).
		Status(m.status).
		Render(m.width, theme)

	sidebarBox := theme.Panel(m.focus == focusList).Render(padLines(
		theme.Title().Render("Projetos")+"\n"+m.tbl.View(), m.sidebarInnerWidth,
	))

	statsTitle := "status"
	if p := m.current(); p != nil {
		statsTitle = p.Name
	}
	statsBox := theme.Panel(false).Render(padLines(
		theme.Title().Render(statsTitle)+"\n"+padToHeight(m.renderStats(), m.statsLines), m.rightInnerWidth,
	))

	descLines := m.currentDescLines()
	descTitle := "descrição"
	if total := len(descLines); total > m.descMaxLines {
		descTitle = fmt.Sprintf("descrição (%d–%d/%d)", m.detailScroll+1, min(m.detailScroll+m.descMaxLines, total), total)
	}
	descBox := theme.Panel(m.focus == focusDescription).Render(padLines(
		theme.Title().Render(descTitle)+"\n"+m.renderDescBody(descLines), m.rightInnerWidth,
	))

	rightCol := lipgloss.JoinVertical(lipgloss.Left, statsBox, descBox)

	var body string
	if m.showGroupsPane {
		groupsBox := theme.Panel(m.focus == focusGroups).Render(padLines(
			theme.Title().Render("Grupos")+"\n"+m.gtbl.View(), m.groupsInnerWidth,
		))
		body = lipgloss.JoinHorizontal(lipgloss.Top, groupsBox, strings.Repeat(" ", panelGap), sidebarBox, strings.Repeat(" ", panelGap), rightCol)
	} else {
		body = lipgloss.JoinHorizontal(lipgloss.Top, sidebarBox, strings.Repeat(" ", panelGap), rightCol)
	}

	view := lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
	if m.settingsModal.Visible() {
		return m.settingsModal.View(theme)
	}
	if m.helpModal.Visible() {
		return m.helpModal.View(theme)
	}
	return view
}

// renderStats is the compact top-right panel: the at-a-glance columns that
// used to live in the sidebar's table (branch/remoto dropped for now, per
// request — more fields can join sujo/push/atividade here later).
func (m appModel) renderStats() string {
	p := m.current()
	if p == nil {
		return theme.Dim().Render("nenhum projeto")
	}
	return strings.Join([]string{
		fmt.Sprintf("Sujo: %s", dirtyCell(*p)),
		fmt.Sprintf("Push: %s", pushCell(*p)),
		fmt.Sprintf("Atividade: %s", humanizeAgo(p.LastCommitTime)),
	}, "\n")
}

// currentDescLines is the full (unscrolled, unclipped) description text for
// current(), wrapped to the panel's width and split into lines — shared by
// renderDescBody, maxDetailScroll, and the title's scroll-position indicator
// so they never disagree about line count.
func (m appModel) currentDescLines() []string {
	p := m.current()
	if p == nil {
		return []string{theme.Dim().Render("nenhum projeto")}
	}

	width := m.rightInnerWidth
	var b strings.Builder
	b.WriteString(theme.Dim().Render(p.Path) + "\n\n")

	switch {
	case !p.IsGit:
		b.WriteString(riskStyle().Render("sem repositório git") + "\n")
	case p.NoCommits:
		b.WriteString(riskStyle().Render("repositório sem nenhum commit ainda") + "\n")
	default:
		b.WriteString(wrapText(fmt.Sprintf("último commit: %s", p.LastCommitMsg), width) + "\n")
	}
	if p.IsGit {
		if p.StashCount > 0 {
			b.WriteString(fmt.Sprintf("%d stash(es)\n", p.StashCount))
		}
		if !p.HasRemote {
			b.WriteString(riskStyle().Render("sem remote configurado — sem backup fora desta máquina") + "\n")
		}
	}

	if p.Description != "" {
		b.WriteString("\n" + theme.Dim().Render(p.DescriptionSrc) + ":\n" + wrapText(p.Description, width) + "\n")
	}

	if len(p.MemoryNotes) > 0 {
		b.WriteString("\n" + theme.Dim().Render("memória do Claude Code:") + "\n")
		for _, note := range p.MemoryNotes {
			b.WriteString(wrapText("• "+note, width) + "\n")
		}
	}

	return strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
}

// maxDetailScroll is the highest detailScroll that still leaves the last
// line visible — scrolling past it would just show trailing blank space.
func (m appModel) maxDetailScroll() int {
	if n := len(m.currentDescLines()) - m.descMaxLines; n > 0 {
		return n
	}
	return 0
}

// renderDescBody clips lines to the panel's fixed descMaxLines budget,
// starting at detailScroll — this is what actually keeps the description
// panel's rendered height constant regardless of content length.
func (m appModel) renderDescBody(lines []string) string {
	scroll := m.detailScroll
	if max := m.maxDetailScroll(); scroll > max {
		scroll = max
	}
	end := scroll + m.descMaxLines
	if end > len(lines) {
		end = len(lines)
	}
	return strings.Join(lines[scroll:end], "\n")
}
