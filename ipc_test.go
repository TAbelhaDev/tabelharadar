package main

import "testing"

func namesOf(out []projectJSON) []string {
	names := make([]string, len(out))
	for i, p := range out {
		names[i] = p.Name
	}
	return names
}

func sameNames(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestFilterProjectsByGroup(t *testing.T) {
	projects := []Project{
		{Name: "tabelharadar"},
		{Name: "tabelhakanban"},
		{Name: "oracle"},
	}
	groups := []groupConfig{
		{Name: "tabeladev", Projects: []string{"tabelharadar", "tabelhakanban"}},
	}

	out, warn := filterProjects(projects, groups, map[string]string{"group": "tabeladev"})
	if warn != "" {
		t.Fatalf("warning = %q, want none", warn)
	}
	if !sameNames(namesOf(out), []string{"tabelharadar", "tabelhakanban"}) {
		t.Fatalf("names = %v, want [tabelharadar tabelhakanban]", namesOf(out))
	}
}

func TestFilterProjectsByGroupAndDirtyANDs(t *testing.T) {
	projects := []Project{
		{Name: "tabelharadar", DirtyCount: 1},
		{Name: "tabelhakanban", DirtyCount: 0},
	}
	groups := []groupConfig{
		{Name: "tabeladev", Projects: []string{"tabelharadar", "tabelhakanban"}},
	}

	out, _ := filterProjects(projects, groups, map[string]string{"group": "tabeladev", "dirty": "true"})
	if !sameNames(namesOf(out), []string{"tabelharadar"}) {
		t.Fatalf("names = %v, want [tabelharadar]", namesOf(out))
	}
}

func TestFilterProjectsUnknownGroupWarnsAndReturnsEmpty(t *testing.T) {
	projects := []Project{{Name: "tabelharadar"}}
	groups := []groupConfig{{Name: "tabeladev", Projects: []string{"tabelharadar"}}}

	out, warn := filterProjects(projects, groups, map[string]string{"group": "does-not-exist"})
	if len(out) != 0 {
		t.Fatalf("out = %v, want empty", out)
	}
	if warn == "" {
		t.Fatal("warning = empty, want a hint that the group doesn't exist")
	}
}

func TestFilterProjectsWithoutGroupFilterUnchanged(t *testing.T) {
	projects := []Project{
		{Name: "tabelharadar", DirtyCount: 1},
		{Name: "oracle", DirtyCount: 0},
	}

	out, warn := filterProjects(projects, nil, map[string]string{"dirty": "true"})
	if warn != "" {
		t.Fatalf("warning = %q, want none", warn)
	}
	if !sameNames(namesOf(out), []string{"tabelharadar"}) {
		t.Fatalf("names = %v, want [tabelharadar]", namesOf(out))
	}
}
