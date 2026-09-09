# AGENTS.md

<!-- tamem:memory-bridge:start -->
## Shared agent memory

This project's persistent notes live at `/home/ianptkcs/agent-memory/tabelharadar`, shared
between Claude Code (which reads/writes it automatically through a
symlink at its own `~/.claude/projects/<escaped-cwd>/memory/`) and any
other agent with no built-in memory feature — that has to be done
manually, following the convention below.

Before starting non-trivial work, read `/home/ianptkcs/agent-memory/tabelharadar/MEMORY.md`
(an index) and any linked topic file relevant to the task.

When you learn something worth remembering — not code patterns or git
history (derivable from the repo), but user preferences, durable
feedback, project state/decisions, or pointers to external systems —
write a new `.md` file in that directory and add a one-line entry to
`MEMORY.md`. Each topic file needs YAML frontmatter:

```yaml
---
name: short-kebab-case-slug
description: one-line summary used to judge relevance later
metadata:
  type: user | feedback | project | reference
---
```

- **user**: the user's role, goals, expertise.
- **feedback**: guidance the user gave about how to approach work
  (what to avoid, what worked) — include *why*.
- **project**: ongoing work/decisions not derivable from the code —
  include *why* and *how to apply*.
- **reference**: pointers to external systems (trackers, dashboards, docs).

Link related entries with `[[other-file-name]]` (without the `.md`
extension). Don't duplicate an existing entry — update it instead.
<!-- tamem:memory-bridge:end -->
