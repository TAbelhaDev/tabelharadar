# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Project groups: a new `[[groups]]` config section names subsets of scanned
  projects, like boards in a kanban. `taradar ipc projects.list group=X`
  filters to a group's members, and a new `taradar ipc groups.list` method
  returns every configured group with its project list.

### Changed

- Renamed the project from `tabelaradar` (TabelaRadar) to `tabelharadar`
  (TAbelhaRadar), following the org-wide `TabelaDev` → `TAbelhaDev` brand
  migration. Module path is now `github.com/TAbelhaDev/tabelharadar`; the
  installed binary is now `taradar` (was `tradar`).

## [v0.4.0] - 2026-08-25

### Changed

- Renamed the installed binary from `tabelharadar` to `tradar`.
  `[digest].kanban_bin` now defaults to `tkanban` (tabelhakanban's new short
  name) instead of `tabelhakanban` — set `kanban_bin` explicitly in
  config.toml if your kanban binary hasn't been renamed yet. Usage/help
  text, the release artifacts and the README updated to match.

## [v0.3.3] - 2026-08-25

### Fixed

- `[digest] enabled = false` is now a real kill switch. It previously only
  forced dry-run (suppressing the kanban write) while still scanning
  projects and still calling the configured LLM provider; it now skips the
  scan, network wait, LLM call and write entirely.

## [v0.3.2] - 2026-08-14

### Adicionado

- `wait_for_network` (default `true`) no `[digest]`: o digest espera a conexão
  com a internet antes de rodar, com `network_timeout` (default 5m) — um timer
  `Persistent` dispara assim que a máquina acorda, antes da rede subir; agora
  ele aborta limpo se a conexão não vier (sem avançar o cursor, o próximo
  timer tenta de novo). Flag `--no-wait` pra pular a espera.
- Schedule default do `--install-timer` movido pra fim do dia
  (`*-*-* 19:00:00`) — mais provável a máquina estar ligada do que de manhã.

## [v0.3.1] - 2026-08-14

### Alterado

- O source de sessões do digest (`opencode_sessions`) memoiza a lista do
  `opencode session list` entre os projetos do board — um board com N projetos
  custava N chamadas de CLI, agora custa uma.

## [v0.3.0] - 2026-08-14

### Adicionado

- **`digest`** — subcomando que vira a atividade dos projetos em updates no
  kanban (`tabelharadar digest`). Coleta commits/estado do git, memória do
  Claude e (opcional, off por default) sessões do opencode; lê o board via
  `tabelhakanban ipc boards.list`; pede a um LLM um plano estruturado
  (`moves`/`updates`/`creates`) e aplica via `cards.move`/`cards.update`/
  `cards.create` — sem nada embutido no kanban, o mapeamento board→projetos é
  config do próprio radar. Tudo decisivo é configurável na seção `[digest]`
  (on/off, dry-run, 5 providers de LLM — opencode/claude CLIs e
  deepseek/openai/anthropic via API —, fontes de atividade, estado, schedule).
  `digest --dry-run` só imprime o plano; `digest --install-timer` cria o
  systemd user timer (`Persistent=true`). Requer `tabelhakanban` ≥ v0.3.0 (o
  método `ipc cards.update`).
- Config em TOML (`~/.config/tabelharadar/config.toml`), substituindo o formato
  de uma-linha-por-caminho. Além de `roots`/`exclude`, agora são configuráveis
  os arquivos de descrição, o dir de memória do Claude, as proporções de
  layout e o editor.
- Tecla `f5`: recarrega config.toml e keybindings sem reiniciar.
- Primeiros testes do repo, cobrindo os dois formatos de config, a precedência
  entre eles, o clamp de valores inválidos e o parsing/plano do digest.

### Alterado

- O arquivo antigo `~/.config/tabelharadar/config` continua sendo lido quando
  não existe `config.toml`, com um aviso apontando pro caminho novo — nenhuma
  instalação existente quebra. Criado o `config.toml`, ele vence sozinho.
