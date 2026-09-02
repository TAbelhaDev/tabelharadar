package main

import (
	"strings"
	"testing"
	"time"
)

func TestParseDigestPlanPlain(t *testing.T) {
	raw := `{"moves":[{"board":"geral","title":"x","from":"a-fazer","to":"feito"}],"updates":[],"creates":[]}`
	plan, err := parseDigestPlan(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Moves) != 1 || plan.Moves[0].Title != "x" || plan.Moves[0].To != "feito" {
		t.Fatalf("plan = %+v, want one move to feito", plan)
	}
	if len(plan.Updates) != 0 || len(plan.Creates) != 0 {
		t.Fatalf("expected empty updates/creates, got %+v", plan)
	}
}

func TestParseDigestPlanFenced(t *testing.T) {
	raw := "claro! aqui vai:\n```json\n{\"updates\":[{\"board\":\"geral\",\"column\":\"fazendo\",\"title\":\"tabelhafin\",\"body\":\"# nova\"}]}\n```\nespero ter ajudado!"
	plan, err := parseDigestPlan(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Updates) != 1 || plan.Updates[0].Body != "# nova" {
		t.Fatalf("plan = %+v, want one update", plan)
	}
}

func TestParseDigestPlanInvalid(t *testing.T) {
	if _, err := parseDigestPlan("sem json nenhum aqui"); err == nil {
		t.Fatal("expected error on input without JSON")
	}
	if _, err := parseDigestPlan("{\"moves\": [broken}"); err == nil {
		t.Fatal("expected error on malformed JSON")
	}
}

func TestParseSince(t *testing.T) {
	now := time.Now()
	cases := map[string]time.Duration{
		"24h":  24 * time.Hour,
		"3d":   3 * 24 * time.Hour,
		"2w":   2 * 7 * 24 * time.Hour,
		"90m":  90 * time.Minute,
		"lixo": 24 * time.Hour, // unparsable → default
	}
	for input, want := range cases {
		got := parseSince(input)
		delta := now.Add(-want).Sub(got)
		if delta > time.Minute || delta < -time.Minute {
			t.Fatalf("parseSince(%q) window off by %v, want ~%v", input, delta, want)
		}
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("curto", 10); got != "curto" {
		t.Fatalf("truncate short = %q, want unchanged", got)
	}
	got := truncate("1234567890abcdef", 5)
	if !strings.HasPrefix(got, "12345") || !strings.HasSuffix(got, "…") {
		t.Fatalf("truncate long = %q, want prefix + ellipsis", got)
	}
}

func TestCompactBoardTruncatesBodies(t *testing.T) {
	long := strings.Repeat("a", 500)
	kb := kanbanBoard{Name: "geral"}
	kb.Columns = append(kb.Columns, struct {
		Name  string `json:"name"`
		Path  string `json:"path"`
		Cards []struct {
			Title string `json:"title"`
			Path  string `json:"path"`
			Body  string `json:"body,omitempty"`
		} `json:"cards"`
	}{Name: "a-fazer", Cards: []struct {
		Title string `json:"title"`
		Path  string `json:"path"`
		Body  string `json:"body,omitempty"`
	}{{Title: "x", Body: long}}})

	out := compactBoard(kb)
	if got := out.Columns[0].Cards[0].Body; len([]rune(got)) > 301 {
		t.Fatalf("compact body length = %d runes, want <= 301", len([]rune(got)))
	}
	if !strings.HasSuffix(out.Columns[0].Cards[0].Body, "…") {
		t.Fatalf("compact body should end with ellipsis")
	}
}

func TestEnabledSourcesRespectsConfig(t *testing.T) {
	all := enabledSources(digestSources{Git: true, ClaudeMemory: true, OpencodeSessions: true})
	if len(all) != 3 {
		t.Fatalf("all sources = %d, want 3", len(all))
	}
	none := enabledSources(digestSources{})
	if len(none) != 0 {
		t.Fatalf("no sources = %d, want 0", len(none))
	}
	onlyGit := enabledSources(digestSources{Git: true})
	if len(onlyGit) != 1 || onlyGit[0].Name() != "git" {
		t.Fatalf("onlyGit = %+v, want just git", onlyGit)
	}
}

// A dry run is a strict no-op: it must not call the kanban runner at all,
// not even to validate a move. Regression for the bug where moveCardOnKanban
// ran inside the if-condition and applied moves during --dry-run.
func TestApplyPlanDryRunDoesNotTouchKanban(t *testing.T) {
	kb := kanbanBoard{Name: "geral"}
	kb.Columns = append(kb.Columns, struct {
		Name  string `json:"name"`
		Path  string `json:"path"`
		Cards []struct {
			Title string `json:"title"`
			Path  string `json:"path"`
			Body  string `json:"body,omitempty"`
		} `json:"cards"`
	}{Name: "a-fazer", Cards: []struct {
		Title string `json:"title"`
		Path  string `json:"path"`
		Body  string `json:"body,omitempty"`
	}{{Title: "x", Body: "# x\n"}}})

	plan := digestPlan{
		Moves:   []digestMove{{Board: "geral", Title: "x", From: "a-fazer", To: "feito"}},
		Updates: []digestUpdate{{Board: "geral", Column: "a-fazer", Title: "x", Body: "# x\n"}},
		Creates: []digestCreate{{Board: "geral", Column: "a-fazer", Title: "y"}},
	}

	calls := 0
	run := func(method string, kv ...string) ([]byte, error) {
		calls++
		return []byte("{}"), nil
	}

	report, applied, failed := applyPlan(kb, plan, true, run)
	if calls != 0 {
		t.Fatalf("dry run called the kanban %d time(s), want 0", calls)
	}
	if applied != 0 || failed != 0 {
		t.Fatalf("dry run applied=%d failed=%d, want 0/0", applied, failed)
	}
	for _, want := range []string{"[dry] mover", "[dry] atualizar", "[dry] criar"} {
		if !strings.Contains(report, want) {
			t.Fatalf("report missing %q:\n%s", want, report)
		}
	}
}

// In a real run the same plan issues the IPC calls and counts them.
func TestApplyPlanRealCallsKanban(t *testing.T) {
	kb := kanbanBoard{Name: "geral"}
	kb.Columns = append(kb.Columns, struct {
		Name  string `json:"name"`
		Path  string `json:"path"`
		Cards []struct {
			Title string `json:"title"`
			Path  string `json:"path"`
			Body  string `json:"body,omitempty"`
		} `json:"cards"`
	}{Name: "a-fazer", Cards: []struct {
		Title string `json:"title"`
		Path  string `json:"path"`
		Body  string `json:"body,omitempty"`
	}{{Title: "x", Body: "# x\n"}}})

	plan := digestPlan{
		Moves:   []digestMove{{Board: "geral", Title: "x", From: "a-fazer", To: "feito"}},
		Creates: []digestCreate{{Board: "geral", Column: "a-fazer", Title: "y", Body: "# y\n"}},
	}

	var methods []string
	run := func(method string, kv ...string) ([]byte, error) {
		methods = append(methods, method)
		return []byte("{}"), nil
	}

	report, applied, failed := applyPlan(kb, plan, false, run)
	if applied != 2 || failed != 0 {
		t.Fatalf("applied=%d failed=%d, want 2/0\n%s", applied, failed, report)
	}
	if len(methods) != 3 || methods[0] != "cards.move" || methods[1] != "cards.create" || methods[2] != "cards.update" {
		t.Fatalf("IPC calls = %v, want [cards.move cards.create cards.update]", methods)
	}
}
