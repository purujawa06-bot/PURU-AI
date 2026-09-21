# AGENTS.md

## Mulai Cepat
- Salin `example.config.json` → `~/.puru/config.json` (`/root/.puru/config.json` untuk root), isi `telegram_bot_token` + `model`.
- `go run . --config ~/.puru/config.json` — jalankan bot (config JSON tunggal, tanpa `.env`, tanpa web UI).
- `go run ./cmd/cli "pesan..."` — chat langsung dengan agent (debug, tanpa Telegram; REPL bila tanpa argumen).
- `go vet ./...` — static analysis.
- `go test ./...` — unit test (ai, config, history, prompt, messages, telegram, openai, memory, app).
- `gofmt -l .` — cek format.

## Konfigurasi (config.json saja)
- Satu-satunya sumber config: JSON (`internal/config/config.go`). Urutan path: flag `--config` > env `PURU_CONFIG` > `~/.puru/config.json`.
- Isi: `telegram_bot_token`, `model{base_url,api_key,model,temperature}`, `workspace` (default `~/.puru/workspace`), `restrict_workspace` (default `true`), `max_iterations` (default `500`), `history_token_limit` (default `30000`), `host` (default `0.0.0.0`) + `port` (default `8080`) khusus health check (`GET /health`), `tools_preview` (default `true`: edit-message live tools yang dipakai AI), `loop_delay_seconds` (default `3`, clamp maks `60`: jeda antar loop/iterasi agent).
- Tidak ada `.env`, web UI, Firebase, settings per-user, providers, combos, relay/proxy, E2B/sandbox, skills, scheduler, vision. Semua itu dihapus.

## Arsitektur (Go, module `github.com/purujawa06-bot/PURU-AI`, lightweight)
- `main.go` — set `debug.SetMemoryLimit(50<<20)` (GOMEMLIMIT 50MB) → load config → satu model OpenAI-compatible (`internal/ai/openai`) → history lokal → agent → Telegram polling + health server (`internal/health`, `GET /health` di `host:port`) di goroutine. Boot cepat: tanpa web server, tanpa Firebase, tanpa init berat.
- `cmd/cli` — wiring sama minus Telegram (debug 1:1).
- `internal/config` — loader JSON + default + validasi + `mkdir workspace/history`.
- `internal/ai` — agent langchaingo (`agents.Executor`, max iterasi = `max_iterations`, jeda antar loop = `LoopDelay()` di `Plan`) + **6 tools** (`tools.go`): `read_file`, `write_file`, `edit_file`, `exec` + `telegram_sendfile` (butuh `Agent.Telegram` + `opts.ChatID`, file di-jail, cap 20MB), `telegram_getuser` (tanpa arg = `opts.User` si penanya; `user_id` = fetch live via `getChat`: nama, id, username, bio; keduanya error-value ramah bila tanpa konteks Telegram/CLI). Tiap tool `Run` dibungkus hook `opts.OnTool(name, args)` untuk preview live. `model.go` = `retryModel`: **setiap pemanggilan API selalu streaming** (agent `Plan` + `Compact` selalu pasang `StreamingFunc` → `stream:true`) dan **API error di-retry total 5x dengan jeda 2 dtk per model call** (tools yang sudah jalan tidak diulang). `fs.go` = jail workspace (`restrict_workspace`: tolak path absolut di luar workspace + `../` escape; berlaku untuk tools DAN `exec.workdir`). `exec.go` = shell dengan timeout: default 60 dtk bila agent tidak mengisi `timeout_seconds`, clamp maks 300 dtk; timeout → kill process group (unix `setpgid` + `SIGKILL`, lihat `exec_unix.go`/`exec_windows.go`) agar tidak bocor RAM. Output di-cap 20k char, file read 30k char.
- `internal/history` — history per-chat di `~/.puru/history/{chatID}.json` (map in-memory + disk). **Tidak pernah di-trim/cap** — tumbuh sampai trigger kompak.
- `internal/memory` — `Compact(ctx, historyText) (rel, err)`: bila history ≥ `history_token_limit` (30k default) SEBELUM prompt baru, model peringkas (TITLE: + bullets, maks 4000 token out) menyimpan `<workspace>/context/YYYY-MM-DD_slug.md` (`# judul`, `Tanggal:`, isi), prune ke 20 file terbaru (by modtime), lalu history di-`Clear` total dan cuma path (`memory.SummaryNote`) yang di-inject sebagai system note — isi TIDAK PERNAH di-inject. **Tidak menyentuh MEMORY.md.** Dipicu di `internal/app` + `cmd/cli`.
- `internal/app` — handler Telegram slim (teks saja, busy-guard per-user, menu terdaftar cuma `/help`/`/clear`/`/token` via `SetMyCommands`; `/token` = `tokenInfo`: % konteks penuh (system prompt render + history incl. tool-call/tool-result/reasoning) vs `history_token_limit` menuju summarize, Markdown fallback, >4096 char → file). `processMessage` mengisi `ProcessOptions{ChatID, User, OnTool}`; `previewHook` edit pesan `🤔 ...` live (`🔧 tool arg`, throttle 1.2 dtk, maks 6 baris) bila `tools_preview=true`.
- `internal/prompt` — system prompt Inggris ringkas (6 tools, aturan workspace, MEMORY.md = fakta permanen yang BOLEH ditulis agent — nama/hobi/info pribadi, tanpa info sementara; ringkasan di `context/`, system-managed).
- `internal/messages`, `internal/tokens`, `internal/telegram`, `internal/ai/openai` — dipertahankan (skema history Vercel-compatible, hitung token o200k_base atas seluruh konteks: system prompt + semua role + tool-call args + tool-result output + reasoning + overhead framing 3/pesan, `Part.ToolCallText`/`ResultText` dipakai bersama request builder agar tak drift, adapter Telegram via **telego** long-polling, transport OpenAI streaming).

## Perilaku Penting
- **Satu model, satu attempt per run**: tanpa fallback provider. Tiap model call streaming + API error di-retry total 5x jeda 2 dtk; gagal total = balasan `Maaf, saya tidak bisa merespons saat ini.` + log `[ai]`.
- **Tool timeout**: wrapper tool 330 dtk (melebihi exec maks 300 dtk); total agent 20 menit.
- **MEMORY.md ditulis agent** (fakta permanen: nama, hobi, info pribadi, preferensi tetap via `write_file`/`edit_file`); compactor DILARANG menyentuhnya; jangan simpan info sementara di sana.
- **Ringkasan di `context/`, inject path saja** — isi ringkasan tidak pernah masuk prompt; AI baca via `read_file` bila perlu.
- **GOMEMLIMIT 50MB**: `debug.SetMemoryLimit` di `main.go`/`cmd/cli` + `ENV GOMEMLIMIT=50MiB` di `Dockerfile`.
- **Workspace**: `restrict_workspace=true` = AI tidak bisa baca/tulis/eksekusi di luar workspace lewat jalur apa pun.

## Konvensi
- Go toolchain 1.26+; format `gofmt`; tidak ada linter wajib selain `go vet`.
- Import internal memakai module path `github.com/purujawa06-bot/PURU-AI/internal/...`.
- Response ke user dalam Bahasa Indonesia & singkat; system prompt bahasa Inggris.
- **Setiap perubahan codebase WAJIB di-update `AGENTS.md` dan `README.md`.**

## Git & Tagging
- **JANGAN commit atau push tanpa diminta eksplisit oleh user.**
- Setiap commit WAJIB diikuti annotated tag (`git tag -a`).
- Format tag `v<major>.<minor>.<patch>` — semver.
- Pesan tag harus berisi penjelasan detail perubahan.
- Sesudah commit & tag: `git push origin main --follow-tags`.
- Tag `v*` memicu workflow `.github/workflows/docker-publish.yml`: publish otomatis ke **GHCR saja** (`ghcr.io/<owner>/puru-ai:latest` + tag versi); push `main` biasa hanya publish `edge` + `sha-<short>`.
