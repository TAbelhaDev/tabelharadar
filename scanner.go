package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Both of these come from config.toml ([scanner]). The defaults are the old
// hardcoded values: ~/.claude/projects, and README first (the common case)
// then the planning-doc names this user's own repos actually use (see
// gosplan's ESCOPO.md/STACK.md, dna's numbered docs), falling back to
// whatever .md exists at all.
func claudeProjectsDir() string { return settings.Scanner.ClaudeProjectsDir }
func mdCandidates() []string    { return settings.Scanner.DescriptionFiles }

type Project struct {
	Name string
	Path string

	IsGit     bool
	NoCommits bool // unborn HEAD: repo exists, nothing committed yet

	Branch      string
	DirtyCount  int
	StashCount  int
	HasRemote   bool
	RemoteURL   string
	HasUpstream bool
	Ahead       int // local commits not yet pushed
	Behind      int // remote commits not yet pulled

	LastCommitTime time.Time
	LastCommitMsg  string

	Description    string
	DescriptionSrc string

	MemoryNotes []string
	MemoryPath  string
	// NextSteps is the full body (frontmatter stripped) of this project's
	// memory file tagged `type: next-steps`, if it has one — see readMemory.
	// Unlike MemoryNotes (one-line index hooks), this is the whole text, for
	// consumers (the ipc subcommand) that want the actual content instead of
	// just a pointer to it.
	NextSteps string
}

// sortsBefore puts projects with uncommitted work first (what you're
// actively touching right now), then orders the rest by most recent commit
// — the two questions this tool exists to answer: "what am I mid-flight
// on" and "what have I not looked at in a while".
func (p Project) sortsBefore(o Project) bool {
	pDirty, oDirty := p.DirtyCount > 0, o.DirtyCount > 0
	if pDirty != oDirty {
		return pDirty
	}
	return p.LastCommitTime.After(o.LastCommitTime)
}

// scanAll resolves every non-excluded root into projects. A root that is
// itself a git repo is added as a single project; otherwise it's treated as
// a repo-group and each of its child directories becomes a project. A root
// that can't be read (missing, no permission) is skipped with a warning
// instead of failing the whole scan — one bad entry in the config shouldn't
// blank out every other monitored repo.
func scanAll(entries []rootEntry) ([]Project, []string) {
	excluded := make(map[string]bool, len(entries))
	for _, e := range entries {
		if e.Exclude {
			excluded[filepath.Clean(e.Path)] = true
		}
	}

	var projects []Project
	var warnings []string
	for _, e := range entries {
		if e.Exclude {
			continue
		}
		root := filepath.Clean(e.Path)
		if excluded[root] {
			continue
		}

		if info, err := os.Stat(filepath.Join(root, ".git")); err == nil && info != nil {
			projects = append(projects, scanOne(filepath.Base(root), root))
			continue
		}

		children, err := os.ReadDir(root)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v", root, err))
			continue
		}
		for _, c := range children {
			if !c.IsDir() || strings.HasPrefix(c.Name(), ".") {
				continue
			}
			path := filepath.Join(root, c.Name())
			if excluded[path] {
				continue
			}
			projects = append(projects, scanOne(c.Name(), path))
		}
	}
	sort.Slice(projects, func(i, j int) bool { return projects[i].sortsBefore(projects[j]) })
	return projects, warnings
}

func scanOne(name, path string) Project {
	p := Project{Name: name, Path: path}

	if info, err := os.Stat(filepath.Join(path, ".git")); err == nil && info != nil {
		p.IsGit = true
		scanGit(&p)
	}

	p.Description, p.DescriptionSrc = findDescription(path)
	p.MemoryNotes, p.MemoryPath, p.NextSteps = readMemory(path)

	return p
}

func runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

func scanGit(p *Project) {
	if _, err := runGit(p.Path, "rev-parse", "--verify", "HEAD"); err != nil {
		p.NoCommits = true
	}

	branch, _ := runGit(p.Path, "branch", "--show-current")
	switch {
	case branch != "":
		p.Branch = branch
	case p.NoCommits:
		p.Branch = "sem commits"
	default:
		p.Branch = "(detached)"
	}

	if status, err := runGit(p.Path, "status", "--porcelain"); err == nil && status != "" {
		p.DirtyCount = len(strings.Split(status, "\n"))
	}

	if stashes, err := runGit(p.Path, "stash", "list"); err == nil && stashes != "" {
		p.StashCount = len(strings.Split(stashes, "\n"))
	}

	if url, err := runGit(p.Path, "remote", "get-url", "origin"); err == nil && url != "" {
		p.HasRemote = true
		p.RemoteURL = url
	}

	if !p.NoCommits && p.HasRemote {
		if _, err := runGit(p.Path, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}"); err == nil {
			p.HasUpstream = true
			if counts, err := runGit(p.Path, "rev-list", "--left-right", "--count", "HEAD...@{u}"); err == nil {
				parts := strings.Fields(counts)
				if len(parts) == 2 {
					p.Ahead, _ = strconv.Atoi(parts[0])
					p.Behind, _ = strconv.Atoi(parts[1])
				}
			}
		}
	}

	if !p.NoCommits {
		// \x1f (unit separator) as the field delimiter: a commit subject
		// can legitimately contain "|", but never a raw control character.
		if out, err := runGit(p.Path, "log", "-1", "--format=%ct%x1f%s"); err == nil {
			if idx := strings.IndexByte(out, 0x1f); idx >= 0 {
				if sec, err := strconv.ParseInt(out[:idx], 10, 64); err == nil {
					p.LastCommitTime = time.Unix(sec, 0)
				}
				p.LastCommitMsg = out[idx+1:]
			}
		}
	}
}

func findDescription(path string) (desc, src string) {
	for _, name := range mdCandidates() {
		if data, err := os.ReadFile(filepath.Join(path, name)); err == nil {
			if d := extractParagraph(string(data)); d != "" {
				return d, name
			}
		}
	}
	matches, _ := filepath.Glob(filepath.Join(path, "*.md"))
	sort.Strings(matches)
	for _, m := range matches {
		if data, err := os.ReadFile(m); err == nil {
			if d := extractParagraph(string(data)); d != "" {
				return d, filepath.Base(m)
			}
		}
	}
	return "", ""
}

// extractParagraph returns the first prose paragraph of a markdown file:
// headings, code fences, and blank lines before it are skipped, and it
// stops at the next blank line so callers get one coherent blurb rather
// than the whole file.
func extractParagraph(content string) string {
	var para []string
	inFence := false
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if trimmed == "" {
			if len(para) > 0 {
				break
			}
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		para = append(para, trimmed)
	}
	desc := strings.Join(para, " ")
	const maxRunes = 240
	runes := []rune(desc)
	if len(runes) > maxRunes {
		desc = string(runes[:maxRunes]) + "…"
	}
	return desc
}

// readMemory pulls the one-line index bullets out of this user's Claude Code
// auto-memory for the project, if this exact path was ever used as a
// session's cwd — memory lives under ~/.claude/projects/<slug>/memory,
// where slug is the absolute path with "/" replaced by "-" (Claude Code's
// own project-directory naming scheme, not something tabelharadar invents). It also
// pulls out the full body of whichever linked memory file is tagged
// `type: next-steps` (see the tabelharadar-next-steps-file convention), since that
// one's actual content — not just its one-line hook — is what the ipc
// subcommand wants to hand an LLM asking "what's left to do here".
func readMemory(path string) (notes []string, memoryPath, nextSteps string) {
	slug := strings.ReplaceAll(path, "/", "-")
	memDir := filepath.Join(claudeProjectsDir(), slug, "memory")
	idxPath := filepath.Join(memDir, "MEMORY.md")
	data, err := os.ReadFile(idxPath)
	if err != nil {
		return nil, "", ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		t := strings.TrimSpace(line)
		if !strings.HasPrefix(t, "- ") {
			continue
		}
		note := strings.TrimPrefix(t, "- ")
		notes = append(notes, note)
		if nextSteps == "" {
			if file := memoryLinkTarget(note); file != "" {
				if body, ok := readNextStepsBody(filepath.Join(memDir, file)); ok {
					nextSteps = body
				}
			}
		}
	}
	return notes, idxPath, nextSteps
}

// memoryLinkTarget pulls the "(file.md)" target out of a "[title](file.md)
// — hook" index bullet.
func memoryLinkTarget(note string) string {
	start := strings.Index(note, "](")
	if start < 0 {
		return ""
	}
	rest := note[start+2:]
	end := strings.Index(rest, ")")
	if end < 0 {
		return ""
	}
	return rest[:end]
}

// readNextStepsBody returns the body of a memory file (its YAML frontmatter
// stripped) if that frontmatter has a `type: next-steps` line, at any
// indentation level (this project's own memory files nest it under
// `metadata:`, but nothing enforces that) — good enough for our own
// convention without pulling in a YAML parser.
func readNextStepsBody(path string) (body string, ok bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	content := string(data)
	if !strings.HasPrefix(content, "---\n") {
		return "", false
	}
	rest := content[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return "", false
	}
	frontmatter := rest[:end]
	isNextSteps := false
	for _, line := range strings.Split(frontmatter, "\n") {
		if strings.TrimSpace(line) == "type: next-steps" {
			isNextSteps = true
			break
		}
	}
	if !isNextSteps {
		return "", false
	}
	return strings.TrimSpace(rest[end+len("\n---"):]), true
}
