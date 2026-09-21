# PURU-AI — lightweight local assistant (Telegram)

Bot Telegram AI Go yang ringan: satu model OpenAI-compatible, agent tool-calling langchaingo dengan **6 tools** yang berjalan di workspace mesin sendiri. Tanpa web UI, tanpa `.env`, tanpa Firebase/VFS/sandbox/skills/scheduler — semua diatur lewat satu `config.json`.

## Fitur

- **6 tools** — `read_file`, `write_file`, `edit_file`, `exec` (workspace, di-jail bila `restrict_workspace: true`: tolak path absolut di luar workspace + `../` escape, berlaku untuk tools dan `exec.workdir`) + `telegram_sendfile` (kirim file workspace ke chat) dan `telegram_getuser` (nama, id, info user: default si penanya, atau lookup user lain via `user_id`; keduanya hanya dalam chat Telegram).
- **Live tool preview** — pesan `🤔 ...` di-edit live (`🔧 nama_tool argumen`) mengikuti tools yang dipakai AI (`tools_preview`, default `true`).
- **Jeda antar loop** — agent berhenti `loop_delay_seconds` (default 3, maks 60) antar iterasi.
- **exec aman** — default timeout 60 dtk bila agent tidak mengisi `timeout_seconds`, clamp maks 300 dtk; saat timeout process group di-kill (unix `setpgid` + `SIGKILL`) agar tidak bocor RAM. Output di-cap 20k char.
- **Memory ringkas** — history per-chat (`~/.puru/history/{chat}.json`) **tidak pernah dipotong**. Setiap prompt baru: bila history ≥ `history_token_limit` (default 30k token), model peringkas menyimpan ringkasan ke `<workspace>/context/YYYY-MM-DD_judul.md` (hanya 20 file terbaru yang disimpan), lalu history dihapus total — ke AI hanya di-inject path file-nya, bukan isinya (AI bisa `read_file` bila butuh konteks lama).
- **MEMORY.md fakta permanen** — `<workspace>/MEMORY.md` hanya berisi fakta awet user (nama, hobi, info pribadi, preferensi tetap) dan boleh ditulis agent sendiri via `write_file`/`edit_file`; compactor tidak pernah menyentuhnya; jangan simpan info sementara di sana.
- **Ringan & boot cepat** — GOMEMLIMIT 50MB (`debug.SetMemoryLimit` + `ENV GOMEMLIMIT=50MiB`), tanpa web server/Firebase/init berat. Max tool iteration default 500 (`max_iterations`).
- **Satu model, satu attempt per run** — tanpa fallback provider. Setiap pemanggilan API selalu streaming, dan API error di-retry total 5x dengan jeda 2 dtk (per model call; tools yang sudah jalan tidak diulang).
- **Health check** — `GET /health` → `{"status":"ok"}` di `host:port` (default `0.0.0.0:8080`), khusus untuk liveness/readiness container.

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
  "history_token_limit": 30000,
  "host": "0.0.0.0",
  "port": 8080,
  "tools_preview": true,
  "loop_delay_seconds": 3
}
```

Urutan path config: flag `--config` > env `PURU_CONFIG` > `~/.puru/config.json`.

## Jalankan

```bash
go run . --config ~/.puru/config.json   # bot Telegram
go run ./cmd/cli "halo"                 # debug CLI (REPL bila tanpa argumen)
go run ./cmd/cli --reset                # hapus history chat debug
```

Perintah Telegram (terdaftar di menu bot): `/help` = bantuan, `/clear` = hapus history, `/token` = info pemakaian token (berapa % menuju summarize otomatis). Kirim teks apa saja untuk chat.

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
