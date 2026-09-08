package main

import "testing"

func TestFilterByGroupMultiGroupMembership(t *testing.T) {
	groups := []groupConfig{
		{Name: "tabeladev", Projects: []string{"tabelharadar", "tabelhakanban"}},
		{Name: "wiv", Projects: []string{"tabelharadar", "oracle"}},
	}
	projects := []Project{
		{Name: "tabelharadar", Path: "/a/tabelharadar"},
		{Name: "tabelhakanban", Path: "/a/tabelhakanban"},
		{Name: "oracle", Path: "/a/oracle"},
	}

	// tabelharadar belongs to both groups: it must show up filtered under each.
	inFirst := filterByGroup(projects, groups, groupEntry{Name: "tabeladev"})
	if !containsName(inFirst, "tabelharadar") || !containsName(inFirst, "tabelhakanban") || containsName(inFirst, "oracle") {
		t.Fatalf("tabeladev filter = %+v, want tabelharadar+tabelhakanban only", inFirst)
	}

	inSecond := filterByGroup(projects, groups, groupEntry{Name: "wiv"})
	if !containsName(inSecond, "tabelharadar") || !containsName(inSecond, "oracle") || containsName(inSecond, "tabelhakanban") {
		t.Fatalf("wiv filter = %+v, want tabelharadar+oracle only", inSecond)
	}
}

func TestFilterByGroupAllEntry(t *testing.T) {
	groups := []groupConfig{
		{Name: "tabeladev", Projects: []string{"tabelharadar"}},
	}
	projects := []Project{
		{Name: "tabelharadar", Path: "/a/tabelharadar"},
		{Name: "solto", Path: "/a/solto"},
	}

	out := filterByGroup(projects, groups, groupEntry{All: true})
	if len(out) != len(projects) {
		t.Fatalf("All filter = %+v, want every project unchanged", out)
	}
}

func TestFilterByGroupNoGroupsConfigured(t *testing.T) {
	projects := []Project{{Name: "solto", Path: "/a/solto"}}

	out := filterByGroup(projects, nil, groupEntry{Name: "qualquer"})
	if len(out) != 1 || out[0].Name != "solto" {
		t.Fatalf("filter with no groups configured = %+v, want projects unchanged", out)
	}
}

func TestFilterByGroupPreservesScanOrder(t *testing.T) {
	groups := []groupConfig{
		{Name: "g", Projects: []string{"c", "a", "b"}},
	}
	// scanAll's own priority order, not alphabetical.
	projects := []Project{
		{Name: "c", Path: "/c"},
		{Name: "a", Path: "/a"},
		{Name: "b", Path: "/b"},
	}

	out := filterByGroup(projects, groups, groupEntry{Name: "g"})
	if len(out) != 3 || out[0].Name != "c" || out[1].Name != "a" || out[2].Name != "b" {
		t.Fatalf("order = %+v, want c,a,b (scan order preserved)", out)
	}
}

// Regression for the cursor/description desync bug: projectRows(visible)[N]
// must correspond to visible[N] itself, and that must hold after switching
// which group is selected (a plain re-sort of m.projects for display, without
// updating what current() indexes, is exactly what broke this before).
func TestProjectRowsMatchesVisibleAfterGroupSwitch(t *testing.T) {
	groups := []groupConfig{
		{Name: "g1", Projects: []string{"z", "y"}},
		{Name: "g2", Projects: []string{"x"}},
	}
	projects := []Project{
		{Name: "z", Path: "/z"},
		{Name: "y", Path: "/y"},
		{Name: "x", Path: "/x"},
	}

	visible := filterByGroup(projects, groups, groupEntry{Name: "g1"})
	rows := projectRows(visible)
	if len(rows) != len(visible) {
		t.Fatalf("rows = %d, visible = %d, want equal length", len(rows), len(visible))
	}
	for i, p := range visible {
		want := statusGlyph(p) + " " + p.Name
		if rows[i][0] != want {
			t.Fatalf("row %d = %q, want %q (visible[%d] = %+v)", i, rows[i][0], want, i, p)
		}
	}

	// Switch groups: visible must be rebuilt from scratch, not reordered
	// in place, and rows must still line up index-for-index.
	visible = filterByGroup(projects, groups, groupEntry{Name: "g2"})
	rows = projectRows(visible)
	if len(visible) != 1 || visible[0].Name != "x" {
		t.Fatalf("g2 visible = %+v, want just x", visible)
	}
	if rows[0][0] != statusGlyph(visible[0])+" x" {
		t.Fatalf("row after switch = %q, want to match visible[0]", rows[0][0])
	}
}

func containsName(projects []Project, name string) bool {
	for _, p := range projects {
		if p.Name == name {
			return true
		}
	}
	return false
}
