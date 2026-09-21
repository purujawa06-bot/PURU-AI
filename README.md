# PURU-AI — lightweight local assistant (Telegram)

Bot Telegram AI Go yang ringan: satu model OpenAI-compatible, agent tool-calling langchaingo dengan **tepat 4 tools lokal** yang berjalan di workspace mesin sendiri. Tanpa web UI, tanpa `.env`, tanpa Firebase/VFS/sandbox/skills/scheduler — semua diatur lewat satu `config.json`.

## Fitur

- **4 tools lokal** — `read_file`, `write_file`, `edit_file`, `exec`. Semua path di-jail ke workspace bila `restrict_workspace: true` (tolak path absolut di luar workspace + `../` escape, berlaku untuk tools dan `exec.workdir`).
- **exec aman** — default timeout 60 dtk bila agent tidak mengisi `timeout_seconds`, clamp maks 300 dtk; saat timeout process group di-kill (unix `setpgid` + `SIGKILL`) agar tidak bocor RAM. Output di-cap 20k char.
- **Memory ringkas** — history per-chat (`~/.puru/history/{chat}.json`) **tidak pernah dipotong**. Setiap prompt baru: bila history ≥ `history_token_limit` (default 30k token), model peringkas menulis ulang `<workspace>/MEMORY.md` (bullet `-` ringkas, tepat 3 seksi: `## Complete`, `## Active`, `## Relevant Files`), lalu history dihapus total bersih.
- **Ringan & boot cepat** — GOMEMLIMIT 50MB (`debug.SetMemoryLimit` + `ENV GOMEMLIMIT=50MiB`), tanpa web server/Firebase/init berat. Max tool iteration default 500 (`max_iterations`).
- **Satu model, satu attempt** — tanpa fallback provider, tanpa retry storm.

## Konfigurasi

Salin dan isi:

```bash
cp example.config.json ~/.puru/config.json  # /root/.puru/config.json untuk root
```

```json
{
  "telegram_bot_token": "123456:ABCDEF",
  "model": {
    "base_url": "https://.../v1",
    "api_key": "sk-...",
    "model": "puru",
    "temperature": 0
  },
  "workspace": "/root/.puru/workspace",
  "restrict_workspace": true,
  "max_iterations": 500,
  "history_token_limit": 30000
}
```

Urutan path config: flag `--config` > env `PURU_CONFIG` > `~/.puru/config.json`.

## Jalankan

```bash
go run . --config ~/.puru/config.json   # bot Telegram
go run ./cmd/cli "halo"                 # debug CLI (REPL bila tanpa argumen)
go run ./cmd/cli --reset                # hapus history chat debug
```

Perintah Telegram: kirim teks apa saja; `/start`/`/menu`/`/help` = bantuan, `/clear`/`/reset` = hapus history.

## Docker

```bash
docker build -t puru-ai .
docker run -d -v puru-data:/root/.puru puru-ai
# atau: -v /root/.puru:/root/.puru agar config + workspace + history persisten
```

### CI/CD

GitHub Actions build & push ke **GHCR (`ghcr.io`)** — tanpa secrets tambahan (pakai `GITHUB_TOKEN` bawaan):

- Push ke `main` → tag `edge` + `sha-<short>`
- Tag release `v*` (mis. `v1.2.3`) → tag `latest` + `v1.2.3`
- Image: `ghcr.io/<owner>/puru-ai` (lowercase)

## Scripts

| Command | Deskripsi |
|---------|-----------|
| `go run .` | Jalankan bot (butuh config.json) |
| `go run ./cmd/cli "pesan"` | Debug CLI tanpa Telegram |
| `go test ./...` | Unit test (ai, config, history, prompt, messages, telegram, openai) |
| `go vet ./...` | Static analysis |
| `gofmt -l .` | Cek format |

## Lisensi

MIT
