package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ianptkcs/tabelatuiui"
)

// kanbanBoard mirrors the wire shape `tkanban ipc boards.list --json`
// returns, so the digest can both show the board to the LLM and validate the
// LLM's proposed operations against it.
type kanbanBoard struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Columns []struct {
		Name  string `json:"name"`
		Path  string `json:"path"`
		Cards []struct {
			Title string `json:"title"`
			Path  string `json:"path"`
			Body  string `json:"body,omitempty"`
		} `json:"cards"`
	} `json:"columns"`
}

// digestPlan is the structured output the LLM is asked for. Everything is
// optional: empty slices mean "nothing to do".
type digestPlan struct {
	Moves   []digestMove   `json:"moves"`
	Updates []digestUpdate `json:"updates"`
	Creates []digestCreate `json:"creates"`
}

type digestMove struct {
	Board string `json:"board"`
	Title string `json:"title"`
	From  string `json:"from"`
	To    string `json:"to"`
}

type digestUpdate struct {
	Board  string `json:"board"`
	Column string `json:"column"`
	Title  string `json:"title"`
	Body   string `json:"body"`
}

type digestCreate struct {
	Board  string `json:"board"`
	Column string `json:"column"`
	Title  string `json:"title"`
	Body   string `json:"body"`
}

// digestState is the persisted cursor in state_file: the last run that
// actually applied something, so the next run only gathers what came after.
type digestState struct {
	LastRun time.Time `json:"last_run"`
}

// runDigest implements `taradar digest [flags]`. Flags:
//
//	--install-timer  write + enable the systemd user timer (from [digest].schedule)
//	--dry-run        force dry-run (never apply, never touch the state file)
//	--no-wait        skip the wait-for-network probe (overrides wait_for_network)
func runDigest(args []string) int {
	forceDryRun, noWait := false, false
	for _, a := range args {
		switch a {
		case "--install-timer":
			return installDigestTimer()
		case "--dry-run":
			forceDryRun = true
		case "--no-wait":
			noWait = true
		}
	}

	cfg := settings.Digest
	// Enabled is a full kill switch: with it off, the digest does nothing at
	// all — no scan, no network wait, no LLM, no write.
	if !cfg.Enabled {
		fmt.Println("digest desabilitado (enabled = false em config.toml) — nada foi feito")
		return 0
	}

	entries, cfgWarning := loadRootsConfig()
	projects, warnings := scanAll(entries)
	if cfgWarning != "" {
		fmt.Fprintln(os.Stderr, "aviso:", cfgWarning)
	}
	for _, w := range warnings {
		fmt.Fprintln(os.Stderr, "aviso:", w)
	}

	// A Persistent timer fires the moment the machine is back — often before
	// the network is up. Wait for connectivity before doing anything network-
	// bound; on timeout, abort cleanly so the cursor doesn't advance and the
	// next timer run retries.
	if cfg.WaitForNetwork && !noWait {
		if err := waitForNetwork(cfg.NetworkTimeout.Duration); err != nil {
			fmt.Fprintln(os.Stderr, "erro:", err)
			return 1
		}
	}

	dryRun := cfg.DryRun || forceDryRun

	llm, err := newLLM(cfg.LLM)
	if err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		return 1
	}

	state := readDigestState(cfg.StateFile)
	windowStart := state.LastRun
	if windowStart.IsZero() {
		windowStart = parseSince(cfg.Sources.Since)
	}

	byName := make(map[string]Project, len(projects))
	for _, p := range projects {
		byName[p.Name] = p
	}

	applied := 0
	failed := 0
	for _, board := range cfg.Boards {
		report, n, f := runBoardDigest(board, byName, cfg, llm, windowStart, dryRun)
		fmt.Print(report)
		applied += n
		failed += f
	}
	if failed > 0 {
		fmt.Fprintf(os.Stderr, "%d operação(ões) falharam\n", failed)
	}

	// Only advance the cursor on a real (non-dry) run, so a preview never
	// swallows activity the next real run should still see.
	if !dryRun {
		if err := writeDigestState(cfg.StateFile, digestState{LastRun: time.Now()}); err != nil {
			fmt.Fprintln(os.Stderr, "aviso: não consegui salvar o estado:", err)
		}
	} else {
		fmt.Println("(dry-run: nada foi aplicado, estado não avançado)")
	}
	return 0
}

// runBoardDigest gathers activity for one mapped board, asks the LLM for a
// plan and applies it. Returns the human-readable report plus how many ops
// applied and how many failed.
func runBoardDigest(board digestBoard, byName map[string]Project, cfg digestConfig, llm llmProvider, windowStart time.Time, dryRun bool) (string, int, int) {
	var b strings.Builder
	fmt.Fprintf(&b, "== board %s ==\n", board.Board)

	sources := enabledSources(cfg.Sources)
	activity := gatherActivity(board, byName, sources, windowStart)
	if len(activity) == 0 {
		b.WriteString("  sem atividade desde a última rodada\n")
	}
	for _, line := range activity {
		fmt.Fprintf(&b, "  %s\n", line)
	}

	if llm == nil {
		b.WriteString("  (IA desligada: só coletei a atividade)\n")
		return b.String(), 0, 0
	}

	kb, ok := readKanbanBoard(board.Board)
	if !ok {
		fmt.Fprintf(&b, "  (board %q não encontrado no kanban — pulando)\n", board.Board)
		return b.String(), 0, 0
	}

	plan, err := askLLM(ctx(), llm, kb, activity)
	if err != nil {
		fmt.Fprintf(&b, "  errou ao pedir plano: %v\n", err)
		return b.String(), 0, 0
	}

	report, applied, failed := applyPlan(kb, plan, dryRun, kanbanIPC)
	b.WriteString(report)
	return b.String(), applied, failed
}

// applyPlan applies a plan to a board through the given kanban runner (in
// tests, a stub). The dryRun contract is strict: a dry run must not call the
// runner at all — it only prints what would happen.
func applyPlan(kb kanbanBoard, plan digestPlan, dryRun bool, run func(method string, kv ...string) ([]byte, error)) (string, int, int) {
	var b strings.Builder
	applied, failed := 0, 0
	for _, op := range plan.Moves {
		if dryRun {
			fmt.Fprintf(&b, "  [dry] mover %q: %s → %s\n", op.Title, op.From, op.To)
			continue
		}
		if moveCardOnKanban(run, kb, op) {
			fmt.Fprintf(&b, "  mover %q: %s → %s\n", op.Title, op.From, op.To)
			applied++
		} else {
			fmt.Fprintf(&b, "  ! mover %q: não achei o card em %q (ou erro)\n", op.Title, op.From)
			failed++
		}
	}
	for _, op := range plan.Updates {
		if dryRun {
			fmt.Fprintf(&b, "  [dry] atualizar %q (coluna %s)\n", op.Title, op.Column)
			continue
		}
		if updateCardOnKanban(run, op) {
			fmt.Fprintf(&b, "  atualizar %q (coluna %s)\n", op.Title, op.Column)
			applied++
		} else {
			fmt.Fprintf(&b, "  ! atualizar %q: não achei o card ou erro\n", op.Title)
			failed++
		}
	}
	for _, op := range plan.Creates {
		if dryRun {
			fmt.Fprintf(&b, "  [dry] criar %q (coluna %s)\n", op.Title, op.Column)
			continue
		}
		if createCardOnKanban(run, op) {
			fmt.Fprintf(&b, "  criar %q (coluna %s)\n", op.Title, op.Column)
			applied++
		} else {
			fmt.Fprintf(&b, "  ! criar %q: erro\n", op.Title)
			failed++
		}
	}
	return b.String(), applied, failed
}

// gatherActivity collects each enabled source's lines for each project of
// the board, in a stable project order.
func gatherActivity(board digestBoard, byName map[string]Project, sources []activitySource, windowStart time.Time) []string {
	var lines []string
	names := append([]string{}, board.Projects...)
	sort.Strings(names)
	for _, name := range names {
		p, ok := byName[name]
		if !ok {
			lines = append(lines, name+": projeto não encontrado no scan do radar")
			continue
		}
		lines = append(lines, fmt.Sprintf("%s (%s):", p.Name, p.Branch))
		for _, s := range sources {
			got, err := s.Gather(context.Background(), p, windowStart)
			if err != nil {
				lines = append(lines, fmt.Sprintf("  [%s] erro: %v", s.Name(), err))
				continue
			}
			for _, l := range got {
				lines = append(lines, "  "+l)
			}
		}
	}
	return lines
}

// readKanbanBoard asks `tkanban ipc boards.list name=<board> --json` and
// returns the first (single) board, or false when the board doesn't exist.
func readKanbanBoard(name string) (kanbanBoard, bool) {
	out, err := kanbanIPC("boards.list", "name="+name)
	if err != nil {
		return kanbanBoard{}, false
	}
	var boards []kanbanBoard
	if err := json.Unmarshal(out, &boards); err != nil || len(boards) == 0 {
		return kanbanBoard{}, false
	}
	return boards[0], true
}

func moveCardOnKanban(run func(method string, kv ...string) ([]byte, error), kb kanbanBoard, op digestMove) bool {
	col := findColumnIn(kb, op.From)
	if col == nil {
		return false
	}
	for _, c := range col.Cards {
		if c.Title == op.Title {
			_, err := run("cards.move", "board="+op.Board, "from="+op.From, "to="+op.To, "title="+op.Title)
			return err == nil
		}
	}
	return false
}

func updateCardOnKanban(run func(method string, kv ...string) ([]byte, error), op digestUpdate) bool {
	_, err := run("cards.update", "board="+op.Board, "column="+op.Column, "title="+op.Title, "body="+op.Body)
	return err == nil
}

func createCardOnKanban(run func(method string, kv ...string) ([]byte, error), op digestCreate) bool {
	if _, err := run("cards.create", "board="+op.Board, "column="+op.Column, "title="+op.Title); err != nil {
		return false
	}
	if op.Body != "" {
		_, err := run("cards.update", "board="+op.Board, "column="+op.Column, "title="+op.Title, "body="+op.Body)
		return err == nil
	}
	return true
}

func findColumnIn(kb kanbanBoard, name string) *struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	Cards []struct {
		Title string `json:"title"`
		Path  string `json:"path"`
		Body  string `json:"body,omitempty"`
	} `json:"cards"`
} {
	for i := range kb.Columns {
		if kb.Columns[i].Name == name {
			return &kb.Columns[i]
		}
	}
	return nil
}

// kanbanIPC runs `tkanban ipc <method> <key=value...> --json` and
// returns the raw JSON stdout. The kanban binary is the digest's plugin
// boundary — everything the digest writes goes through this one place.
func kanbanIPC(method string, kv ...string) ([]byte, error) {
	bin := settings.Digest.KanbanBin
	if bin == "" {
		bin = "tkanban"
	}
	args := append([]string{"ipc", method}, append(append([]string{}, kv...), "--json")...)
	cmd := exec.Command(bin, args...)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s %s: %v (%s)", bin, method, err, strings.TrimSpace(errBuf.String()))
	}
	return out.Bytes(), nil
}

// askLLM builds the prompt from the board + activity and asks the configured
// provider for a structured plan.
func askLLM(ctx context.Context, llm llmProvider, kb kanbanBoard, activity []string) (digestPlan, error) {
	boardJSON, _ := json.Marshal(compactBoard(kb))
	system := `Você é o assistente do TAbelhaDigest, que mantém um kanban atualizado a partir da atividade real dos projetos.

Você recebe: (1) o estado atual de um board do kanban em JSON, e (2) a atividade recente dos projetos mapeados a esse board.

Sua tarefa: produzir mudanças que reflitam a atividade. Regras:
- Aja APENAS com base na atividade fornecida; não invente progresso.
- Movimentos plausíveis: card de coluna de "a fazer" pra "em andamento" quando houver trabalho claro em curso; pra coluna de "feito"/"done" quando a atividade indicar conclusão.
- Atualizações: reescreva o body de um card preservando o que já existe (checklists e notas), adicionando uma seção curta de progresso com a data de hoje. Se o body já tiver uma seção "## Progresso", SUBSTITUA-a pela nova em vez de acumular seções duplicadas. Não inclua front matter.
- Criação: só crie card quando a atividade indicar um tópico/projeto novo que não tem card.
- Em moves/updates, o título do card deve ser EXATAMENTE igual ao existente no board.
- Responda APENAS com JSON válido, sem markdown, neste formato exato:
{"moves":[{"board":"...","title":"...","from":"...","to":"..."}],"updates":[{"board":"...","column":"...","title":"...","body":"..."}],"creates":[{"board":"...","column":"...","title":"...","body":"..."}]}
- Arrays vazios quando nada a fazer. "from"/"column"/"to" devem ser nomes reais de colunas do board fornecido.`
	user := fmt.Sprintf("Hoje é %s.\n\nBoard atual (JSON):\n%s\n\nAtividade recente dos projetos do board:\n%s\n\nRetorne o JSON das mudanças.",
		time.Now().Format("2006-01-02"), boardJSON, strings.Join(activity, "\n"))

	raw, err := llm.Complete(ctx, system, user)
	if err != nil {
		return digestPlan{}, err
	}
	return parseDigestPlan(raw)
}

// compactBoard is a trimmed copy of the board for the prompt: bodies capped
// so a long card body doesn't blow up the context.
func compactBoard(kb kanbanBoard) kanbanBoard {
	out := kanbanBoard{Name: kb.Name, Path: kb.Path}
	for _, c := range kb.Columns {
		col := struct {
			Name  string `json:"name"`
			Path  string `json:"path"`
			Cards []struct {
				Title string `json:"title"`
				Path  string `json:"path"`
				Body  string `json:"body,omitempty"`
			} `json:"cards"`
		}{Name: c.Name, Path: c.Path}
		for _, card := range c.Cards {
			card.Body = truncate(card.Body, 300)
			col.Cards = append(col.Cards, card)
		}
		out.Columns = append(out.Columns, col)
	}
	return out
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "…"
}

// parseDigestPlan extracts the first JSON object out of the LLM's reply,
// tolerating ```json fences and stray prose before/after it.
func parseDigestPlan(raw string) (digestPlan, error) {
	var plan digestPlan
	s := raw
	if i := strings.Index(s, "```"); i >= 0 {
		s = s[:i] + s[i+3:]
	}
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start < 0 || end <= start {
		return plan, fmt.Errorf("resposta do LLM sem objeto JSON")
	}
	if err := json.Unmarshal([]byte(s[start:end+1]), &plan); err != nil {
		return plan, fmt.Errorf("JSON inválido na resposta do LLM: %v", err)
	}
	return plan, nil
}

// readDigestState loads the persisted cursor; a missing/corrupt file is a
// zero state (first run), never an error.
func readDigestState(path string) digestState {
	data, err := os.ReadFile(path)
	if err != nil {
		return digestState{}
	}
	var s digestState
	if json.Unmarshal(data, &s) != nil {
		return digestState{}
	}
	return s
}

func writeDigestState(path string, s digestState) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// parseSince turns the [digest.sources].since string ("24h", "7d", "2w") into
// a windowStart, defaulting to 24h on anything unparsable.
func parseSince(s string) time.Time {
	var d time.Duration
	switch {
	case strings.HasSuffix(s, "d"):
		var n int
		if _, err := fmt.Sscanf(s, "%dd", &n); err == nil && n > 0 {
			d = time.Duration(n) * 24 * time.Hour
		}
	case strings.HasSuffix(s, "w"):
		var n int
		if _, err := fmt.Sscanf(s, "%dw", &n); err == nil && n > 0 {
			d = time.Duration(n) * 7 * 24 * time.Hour
		}
	default:
		if dd, err := time.ParseDuration(s); err == nil && dd > 0 {
			d = dd
		}
	}
	if d <= 0 {
		d = 24 * time.Hour
	}
	return time.Now().Add(-d)
}

func ctx() context.Context { return context.Background() }

// waitForNetwork probes github.com until a connection succeeds or the timeout
// expires — the same guard the user's cron jobs use (wait_for_net), in-process
// so the timer's ExecStart stays a single `taradar digest` call.
func waitForNetwork(timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 5 * time.Second}
	for time.Now().Before(deadline) {
		resp, err := client.Get("https://github.com")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode < 500 {
				return nil
			}
		}
		time.Sleep(5 * time.Second)
	}
	return fmt.Errorf("sem conexão com a internet após %s, abortando (o timer vai tentar de novo)", timeout)
}

// installDigestTimer writes and enables the systemd user units that run the
// digest on a schedule, using [digest].schedule (OnCalendar/Persistent). The
// units are the same one-shot convention as the rest of the user's jobs
// (Persistent=true so a missed fire while the machine is off reruns on boot).
func installDigestTimer() int {
	cfg := settings.Digest
	bin, err := os.Executable()
	if err != nil || bin == "" {
		if bin, err = exec.LookPath("taradar"); err != nil {
			fmt.Fprintln(os.Stderr, "erro: não achei o binário do taradar")
			return 1
		}
	}

	dir := filepath.Join(tuiui.ConfigDir(), "systemd", "user")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		return 1
	}

	servicePath := filepath.Join(dir, "tabelharadar-digest.service")
	timerPath := filepath.Join(dir, "tabelharadar-digest.timer")

	logPath := filepath.Join(filepath.Dir(cfg.StateFile), "digest.log")
	service := `[Unit]
Description=tabelharadar digest (kanban auto-update)

[Service]
Type=oneshot
TimeoutStartSec=infinity
ExecStart=` + bin + ` digest
StandardOutput=append:` + logPath + `
StandardError=append:` + logPath + `
`

	persistent := "no"
	if cfg.Schedule.Persistent {
		persistent = "yes"
	}
	timer := `[Unit]
Description=tabelharadar digest (schedule)

[Timer]
OnCalendar=` + cfg.Schedule.OnCalendar + `
Persistent=` + persistent + `

[Install]
WantedBy=timers.target
`

	if err := os.WriteFile(servicePath, []byte(service), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		return 1
	}
	if err := os.WriteFile(timerPath, []byte(timer), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		return 1
	}

	for _, args := range [][]string{
		{"--user", "daemon-reload"},
		{"--user", "enable", "--now", "tabelharadar-digest.timer"},
	} {
		if out, err := exec.Command("systemctl", args...).CombinedOutput(); err != nil {
			fmt.Fprintln(os.Stderr, "systemctl:", err, string(out))
			return 1
		}
	}

	fmt.Printf("timer instalado: %s (OnCalendar=%s, Persistent=%s)\n", timerPath, cfg.Schedule.OnCalendar, persistent)
	fmt.Println("veja o estado com: systemctl --user list-timers tabelharadar-digest.timer")
	return 0
}
