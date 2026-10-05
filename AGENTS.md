# AGENTS.md — PURU-AI

Single Go binary Telegram bot. Module `github.com/purujawa06-bot/PURU-AI`, `go 1.26` (`golang:1.26-alpine` in `Dockerfile`, Go 1.26 in CI).

Code rule: English-only names, comments, logs, and error messages (no external skill required).

## Mandatory Workflow

- Before writing any code, read the `ponytail` skill (full) and the `rules-write-code` skill first.
- Respond in Indonesian.

## Commands

```bash
go test ./...                          # full suite
go test ./internal/ai -run TestFoo -v  # single test
go vet ./...                           # run before test in release verify
gofmt -s -w .                          # only formatter (no lint config)
go build -o puru-ai .                  # raw binary, then ./puru-ai --config config.json
go run ./cmd/puru chat "hello"         # local debug, no Telegram (REPL when no args)
```

Release is manual only (`.github/workflows/release.yml`, Run workflow with `bump` = major/minor/patch): `vet → test → build + node --check npm/bin/puru.js npm/scripts/install.js npm/scripts/binary.js` (fail = stop) → push `v*` tag → Docker GHCR → GitHub Release → npm publish. Logs pushed to `info` branch.

## Entrypoints & config

- `main.go`: raw binary — long-polling loop + goroutine serving only `GET /health` → `{"status":"ok"}` (`internal/health`). Deletes webhook on boot, exits after 5× 409 conflicts.
- `cmd/puru/main.go`: shipped CLI — `puru setup` (wizard, writes config; non-interactive via `TELEGRAM_BOT_TOKEN`, `PURU_BASE_URL`, `PURU_API_KEY`, `PURU_MODEL`, `PURU_WORKSPACE`; `--force` overwrites) → `puru gateway` (NO web server unless `--health`) → `puru chat` (local debug). npm `puru` bin downloads this binary from GitHub Releases; `PURU_AI_BINARY` env overrides the path.
- `cmd/cli/main.go`: older debug CLI (`--config --chat` default `-777` `--reset`); wrappers `cli.sh` / `cli.bat`.
- Config is one JSON (`cp example.config.json config.json`; example defaults to `https://api.openai.com/v1` / `gpt-4o-mini`). Resolution: `--config` > `$PURU_CONFIG` > `~/.puru/config.json`. See `internal/config/config.go:Load`.
- Required: `telegram_bot_token`, `model.base_url`, `model.model`. Defaults: `workspace` → `<DefaultDir>/workspace`, `max_iterations` 500, `exec_memory_mb` min/clamp 64, `tools_preview` true when unset (pointer), `loop_delay_seconds` 3 (max 60), `skills_mode` default (off = no skill sections in prompt, custom = only `skills_allow`).

## Architecture (`internal/`)

- `ai/`: agent loop, single OpenAI-compatible model, no fallback. 14 tools by default: `read_file`, `write_file`, `list_dir`, `grep`, `edit_file`, `append_file`, `run_shell_command`, `telegram_sendfile`, `telegram_getuser`, `get_env`, `web_fetch`, `manage_schedule`, `use_skill`, `stop_skill` + opt-in `web_search` (15th, only when at least one `web_search` provider is ready: `aistudio` needs active + model + api key, `exa` needs active + api key). Order is fixed: `aistudio` (0), `exa` (1) — first ready error falls through to the next ready one. Timeouts: 330s per tool, 20m total (`agent.go`); `web_search` 60s, `web_fetch` 120s. Retry 5×/2s happens inside the model call (`model.go:retryModel`) — never re-run failed tools.
- `ai/web.go`: `web_search` via Google AI Studio Gemini `generateContent` + `googleSearch` grounding (`aistudioAPIBase`, mockable var in tests; model free-form, e.g. `gemini-2.5-flash` / `gemma-4-31b-it`) with Exa fallback (`POST /search` with `x-api-key`, `type:auto` + `highlights`, `exaAPIBase` mockable var); removed from tool list unless at least one provider is ready. `web_fetch` strips HTML to text and paginates by lines (`start_line`/`length`, 100 lines default, max 1000 per call; rejects local/private hosts).
- `app/` Telegram handling; `config/` load/validate + `HistoryDir()` + `MemoryPath()` (`memory/MEMORY.md`); `history/` per-chat JSON in `~/.puru/history`; `memory/` summarization into `memory/context/` (newest 20, `LatestSummary` injected); `workspace/` picoclaw-style bootstrap (`AGENTS.md` preferring plural, `AGENT.md` legacy alias, `SOUL.md`, `USER.md`; leading YAML frontmatter is stripped, `skills: [...]` activates skills) + embedded builtin skills (`skills/find-skills`, `skills/skill-creator`, seeded by `Ensure`, never overwritten) + skill catalog (`skills/*/SKILL.md`, `LoadSkill`/`LoadSkillsForContext`/`BuildSkillsSummary`); `prompt/` renders catalog + Active Skills 1:1 picoclaw (no install tutorial inline — it lives in the `find-skills` SKILL.md), `messages/`, `tokens/`, `telegram/`.
- `restrict_workspace=true` jails file tools (rejects absolute/`../` escapes) and forces `run_shell_command` Dir inside workspace.

## Conventions

- All user/agent-visible strings are English. The system prompt (`internal/prompt/prompt.go`) already tells the agent to reply in the user's language — never hardcode Indonesian into outputs, commands, or `example.config.json` placeholders.
- Never prune history: `PruneMessages`/`PruneTurn` are intentional no-ops, `SanitizeHistoryMessages` only truncates 8k-char messages. Trimming happens solely via `memory.Compact` at `history_token_limit`, keeping the last user+assistant exchange (`messages.KeepLastExchange`) plus the fresh summary.
- Number formatting is EN style (`fmtInt` → `30,000`, `fmtPct` → `50.0%`); tests assert this.

## Gotchas

- Heap capped at 50MB (`debug.SetMemoryLimit` in all three mains + `GOMEMLIMIT=50MiB` in `Dockerfile`). Keep dependencies small.
- `run_shell_command` RAM cap is Linux-only (RSS poll + SIGKILL process group, `exec_mem_linux.go`); on Windows/macOS (`exec_mem_other.go`) only timeout + output/file-size caps apply.
- History/memory live outside the repo (`~/.puru/`); Docker persists `/root/.puru` volume. Never commit `config.json` or tokens — `example.config.json` uses placeholders.
