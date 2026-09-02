<div align="center">

# TAbelhaRadar

**TUI que fiscaliza a saúde git dos seus repositórios locais** — WIP, commits
não enviados, repos sem remote, projetos parados há tempo demais.

[English](README.md) · **Português**

[![Go Version](https://img.shields.io/github/go-mod/go-version/TAbelhaDev/tabelharadar?style=flat-square&logo=go&logoColor=white&color=00ADD8)](go.mod)
[![Built with Bubble Tea](https://img.shields.io/badge/built%20with-Bubble%20Tea-ff69b4?style=flat-square)](https://github.com/charmbracelet/bubbletea)
[![Powered by tabelatuiui](https://img.shields.io/badge/theme-tabelatuiui-d6b4f7?style=flat-square)](https://github.com/TAbelhaDev/tabelatuiui)
[![License: AGPL-3.0](https://img.shields.io/badge/license-AGPL--3.0-blue?style=flat-square)](LICENSE)

[![ko-fi](https://ko-fi.com/img/githubbutton_sm.svg)](https://ko-fi.com/ianptkcs)

</div>

---

## O que é

Um [Bubble Tea](https://github.com/charmbracelet/bubbletea) TUI que varre
os repositórios/pastas-de-repositórios listados nas settings (por padrão só
`~/codigo/pessoal`) e fiscaliza o estado de cada projeto: o que está em WIP,
o que tem commits não enviados, o que não tem remote configurado (logo, sem
backup fora da máquina) e o que já não é tocado há um tempo — a inspeção
disciplinar dos seus repositórios.

Não existe nenhum arquivo de metadado separado — todo o estado é inferido
do próprio git de cada repositório, mais o primeiro parágrafo de prosa do
seu `README.md`/`PLANNING.md`/`ESCOPO.md`/`STACK.md`/`TODO.md`/`CLAUDE.md`
(o primeiro que existir), mais os bullets do índice de memória do Claude
Code daquele projeto (`~/.claude/projects/<slug>/memory/MEMORY.md`), quando
uma sessão já rodou ali dentro.

O tema e o chrome compartilhado (header/footer/panels, padding ANSI-aware, os
helpers de `ipc ... --json`) vêm da
[`tabelatuiui`](https://github.com/TAbelhaDev/tabelatuiui), a lib de UI
compartilhada dos meus TUIs Bubble Tea.

## Índice

- [Instalação](#instalação)
- [Layout](#layout)
- [Uso](#uso)
- [IPC](#ipc)
- [Digest](#digest)
- [Configuração](#configuração)
- [Licença](#licença)

## Instalação

Requer Go 1.26+.

```bash
go install github.com/TAbelhaDev/tabelharadar@latest
```

Isso instala o binário como `tabelharadar` (nome do módulo). Pra ter o nome curto
`taradar` usado no resto deste README, compile a partir do source:

```bash
git clone https://github.com/TAbelhaDev/tabelharadar.git
cd tabelharadar
go build -o taradar .
```

## Layout

Três painéis:

- **Projetos** (esquerda, 1/5 da largura) — só os nomes, pra navegar rápido
  pela lista inteira. O glyph antes do nome resume o status: `○` sem git,
  `●` com mudanças não commitadas, `▲` com commits não enviados, `✕` com
  commits mas sem remote, `✓` limpo e sincronizado.
- **status** (topo direita, 1/5 da altura) — sujo (arquivos não
  commitados), push (commits não enviados) e atividade (há quanto tempo
  desde o último commit) do projeto selecionado.
- **descrição** (embaixo, 4/5 da altura) — o resto: path, último commit,
  avisos (sem remote, stash), descrição extraída do README/PLANNING/etc. e
  bullets da memória do Claude Code daquele projeto. Quando o conteúdo não
  cabe, o título mostra o intervalo visível (`descrição (1–13/27)`) e dá
  pra rolar.

## Uso

```
taradar         # abre a TUI
taradar list    # dump em texto plano, sem TTY — útil pra scriptar
taradar digest  # vira atividade recente em updates no kanban (precisa de [digest] no config)
```

Dentro da TUI: `↑`/`↓` (ou `j`/`k`) navegam a lista de projetos,
`ctrl+h`/`ctrl+l` alternam entre o painel de projetos e o de descrição,
`j`/`k` (ou `↑`/`↓`) rolam o texto da descrição quando ela está focada,
`o`/`enter` abre o projeto selecionado no `$EDITOR` (padrão `nvim`), `r`
reescaneia, `q` sai.

## IPC

Pra scripts ou pra um LLM perguntar "o que falta fazer, onde eu parei em
cada projeto, o que dá pra começar a implementar" sem abrir a TUI,
`taradar` expõe um subcomando `ipc` não-interativo, no mesmo espírito do
`dcal ipc <método> --json`/`djobs ipc <método> --json`:

```bash
taradar ipc projects.list --json                  # todo projeto trackeado, com status git + descrição + próximos passos
taradar ipc projects.list dirty=true --json       # só quem tem mudanças não commitadas
taradar ipc projects.list name=tabelhacal --json   # um projeto específico
taradar ipc projects.next --json                  # o projeto que o próprio taradar priorizaria (mid-flight > mais recente)
```

Cada projeto no JSON traz, além dos campos de status git (branch, sujo,
ahead/behind, último commit), `description` (extraída do README/PLANNING/
etc.), `memory_notes` (os hooks de uma linha do índice de memória) e
`next_steps` — o corpo **inteiro** (não só o hook truncado) de qualquer
memória daquele projeto marcada `type: next-steps` na sua própria
`~/.claude/projects/<slug>/memory/` — vazio se o projeto ainda não tiver
uma.

## Digest

`taradar digest` vira atividade recente dos projetos em updates no kanban —
o "pra quê" do radar. Ele coleta a atividade dos projetos mapeados, pede a um
LLM um plano estruturado e aplica no kanban via IPC. Nada mora dentro do
kanban: o mapeamento board→projetos é config do próprio radar.

Fluxo por rodada:

1. coleta atividade de cada projeto dos `[digest.boards]` desde a última
   rodada — commits + estado do git, o índice de memória do Claude e
   (opcional, off por default) sessões recentes do opencode via
   `opencode session list`;
2. lê o estado atual do board com `tkanban ipc boards.list`;
3. pede ao LLM configurado um plano: `{"moves":[...],"updates":[...],"creates":[...]}`;
4. aplica com `tkanban ipc cards.move` / `cards.update` / `cards.create`
   (ou só imprime com `--dry-run` / `dry_run = true`).

```bash
taradar digest                 # aplica (precisa de [digest] com enabled = true)
taradar digest --dry-run       # imprime o plano, não muda nada
taradar digest --install-timer # timer systemd de usuário a partir de [digest].schedule
```

O cursor mora em `state_file` (`~/.local/state/tabelharadar/digest.json` por
default) e só avança numa rodada real, não-dry — assim um preview nunca engole
atividade. Requer `tabelhakanban` ≥ v0.3.0 (o método `ipc cards.update`).

Um `[digest]` mínimo:

```toml
[digest]
enabled = true
wait_for_network = true   # espera a conexão antes de rodar (default true)
network_timeout = "5m"

[digest.llm]
provider = "opencode"   # opencode | claude | deepseek | openai | anthropic

[digest.sources]
git = true
claude_memory = true

[[digest.boards]]
board = "geral"
projects = ["tabelhacal", "tabelhafin"]

[digest.schedule]       # usado por `digest --install-timer`
on_calendar = "*-*-* 19:00:00"
```

`wait_for_network` existe porque um timer `Persistent` dispara no momento em
que a máquina volta — normalmente antes da rede subir. O digest faz um probe
no github.com (até `network_timeout`) e, se a conexão não vier, aborta limpo
sem avançar o cursor — o próximo timer tenta de novo. `--no-wait` pula a
espera numa rodada manual.

Tudo é um dial: `enabled` é o botão de desligar — com ele off, `digest` não
faz nada (sem scan, sem LLM, sem escrita); `dry_run` (só relevante com
enabled ligado) decide se ela escreve, `[digest.llm]` como, `[digest.sources]`
o que ela enxerga e `[[digest.boards]]` quais boards alimentam quais projetos.

## Configuração

Tudo fica em `~/.config/tabelharadar/config.toml` (sobrescrível via
`TABELHARADAR_CONFIG`). O arquivo é opcional e parcial: só as chaves presentes
sobrescrevem, o resto segue no default. `f5` recarrega sem reiniciar.

```toml
roots = ["~/codigo/pessoal", "~/codigo/tabeladev"]
exclude = ["~/codigo/pessoal/spotdash"]

[scanner]
description_files = ["README.md", "PLANNING.md", "ESCOPO.md", "STACK.md", "TODO.md", "CLAUDE.md"]
claude_projects_dir = "~/.claude/projects"

[layout]
sidebar_width_share = 1  # razão de LARGURA sidebar:coluna-direita
right_width_share   = 4
stats_height_share  = 1  # razão de ALTURA stats:descrição
desc_height_share   = 4

[general]
editor = "nvim"  # vazio = usa $EDITOR, depois nvim

# As chaves do digest são todas opcionais; o quadro completo está na seção Digest.
[digest]
enabled = false    # IA atualizando o kanban — opt-in
dry_run = false    # true = imprime o plano, não escreve nada
wait_for_network = true  # espera a conexão antes de rodar (default true)
network_timeout = "5m"   # quanto esperar (default 5m)

[digest.llm]
provider = "opencode"  # opencode | claude | deepseek | openai | anthropic
# model = "..."        # só providers HTTP (default de cada um quando vazio)
# base_url = ""        # override pra gateways compatíveis com OpenAI
# api_key_env = ""     # DEEPSEEK_API_KEY / OPENAI_API_KEY / ANTHROPIC_API_KEY
# timeout = "120s"
# max_tokens = 0       # 0 = default do provider
# temperature = 0.2

[digest.sources]
git               = true   # commits desde a última rodada + estado atual
claude_memory     = true   # índice MEMORY.md + next-steps
opencode_sessions = false  # sessões recentes do opencode (via CLI do opencode)
# since = "24h"            # janela na primeira rodada, antes de existir state file

[[digest.boards]]          # repita pra cada board
board = "geral"
projects = ["tabelhacal", "tabelhafin"]

[digest.schedule]          # usado por `digest --install-timer`
on_calendar = "*-*-* 09:00:00"
persistent  = true
```

### Quais repos monitorar

`roots` aceita dois tipos de caminho:

- uma pasta-de-repos: cada subpasta com `.git` vira uma linha na tabela (é
  como `~/codigo/pessoal` funciona);
- um repositório git direto: entra sozinho, como uma linha só (útil pra um
  repo solto fora das pastas padrão).

`exclude` esconde um caminho específico — seja uma raiz inteira, seja um filho
de uma raiz citada em `roots`. A ordem não importa entre as duas listas.

Sem nenhum arquivo, varre só `TABELHARADAR_ROOT` (ou `~/codigo/pessoal`).

### Migrando do formato antigo

Antes da 0.3.0 a config era `~/.config/tabelharadar/config`, uma entrada por
linha com `!` prefixando exclusões. **Esse arquivo continua sendo lido** quando
não existe `config.toml`, com um aviso na barra de status. A tradução é direta:

```
~/codigo/pessoal              →  roots   = ["~/codigo/pessoal"]
!~/codigo/pessoal/spotdash    →  exclude = ["~/codigo/pessoal/spotdash"]
```

Criado o `config.toml`, ele passa a valer sozinho — os dois formatos não se
misturam, e o arquivo antigo pode ser apagado.

### Outras variáveis

- `TABELHARADAR_CONFIG` — caminho do `config.toml`, se não for o padrão.
- `TABELHARADAR_ROOT` — diretório varrido quando não existe config nenhuma
  (padrão `~/codigo/pessoal`).
- `TABELHARADAR_ACCENT` — accent Catppuccin Mocha manual, usado só quando o
  DankMaterialShell não está instalado/configurado (padrão `mauve`).
- `TABELHARADAR_DMS_SETTINGS` — caminho do `settings.json` do DMS, se não for
  o padrão.

## Desenvolvimento

```bash
go test ./...
```

## Changelog

Veja [CHANGELOG.md](CHANGELOG.md) para o histórico de versões.

## Apoie o projeto

- **Global**: [ko-fi.com/ianptkcs](https://ko-fi.com/ianptkcs)
- **Brasil (Pix)**: escaneie o QR abaixo ou copie o código

  <img src="pix-qr.png" alt="Pix QR" width="200" />

  <details><summary>Código Pix (copiar)</summary>

  ```
  00020126580014BR.GOV.BCB.PIX01365ad933b0-dcdc-4525-a736-0759902aeec65204000053039865802BR5925Ian Patrick da Costa Soar6009SAO PAULO62140510tQA85x6Dov63041FB6
  ```

  </details>

## Licença

[GNU AGPL-3.0](LICENSE) — livre e open source. Se você rodar uma versão
modificada deste projeto, inclusive como serviço de rede, também precisa
disponibilizar o código-fonte modificado sob a mesma licença.
