---
name: puru-bootstrap
description: Run a Puru self-improvement bootstrap round via cmd/cli. Use when user says bootstrap, bootstrapping, self-improve, ronde, or asks Puru to improve itself.
---

# Puru Bootstrap

Run one bounded self-improvement round: Puru reads its own codebase (workspace = repo root) and implements ONE small, safe improvement.

## Setup

Config file (create if missing, e.g. `C:/Users/LENOVO/AppData/Local/Temp/opencode/puru-bootstrap-config.json`):

```json
{
  "telegram_bot_token": "test-dummy-token",
  "telegram_allowed_users": [],
  "model": { "base_url": "https://puruboy-api.vercel.app/api", "api_key": "", "model": "auto", "temperature": 0 },
  "workspace": "<ABSOLUTE REPO ROOT>",
  "restrict_workspace": true,
  "max_iterations": 500,
  "history_token_limit": 30000,
  "host": "0.0.0.0",
  "port": 8080,
  "tools_preview": true,
  "loop_delay_seconds": 1,
  "exec_memory_mb": 64
}
```

`workspace` MUST be the repo root so Puru can read/edit its own code. `max_iterations` 500.

## Run

1. Pick a fresh negative chat ID per round (e.g. `-2005`, increment each round).
2. Reset: `go run ./cmd/cli --config "<cfg>" --chat <ID> --reset`
3. Run the round (single Bash call, `--chat <ID>`):

> Ronde N bootstrapping (max 500 langkah, manfaatkan seperlunya). Workspace ini adalah codebase-mu sendiri (repo PURU-AI). Konteks: baca SELF_IMPROVE.md dan jalankan exec git diff --stat + git status untuk lihat perubahan ronde sebelumnya yang belum di-commit. Tugas: 1) Pilih SATU peningkatan berikutnya yang paling berdampak (lihat rencana di SELF_IMPROVE.md; bila semua selesai, cari bug/inkonsistensi baru via read_file ke tools.go/fs.go/exec.go/agent.go/memory.go/prompt.go). 2) Implementasikan langsung di code — kecil, aman, teruji; JANGAN merusak build (periksa tipe/fungsi yang ada dulu via read_file sebelum edit). 3) Update SELF_IMPROVE.md (edit_file/append_file) dengan catatan ronde berikutnya sesuai urutan yang belum dipakai. 4) Bila ubah code, update AGENTS.md dan README.md sesuai konvensi. Jangan hapus file, jangan ubah config/example.config, jangan git commit. Akhiri dengan ringkasan perubahan.

4. If output ends with `Batas langkah tercapai`, continue with the SAME chat ID: `go run ./cmd/cli --config "<cfg>" --chat <ID> "lanjut"`. Repeat until Puru gives its summary.

## After

Hand off to the `puru-verify` skill: review `git status`/`git diff`, fix build breakage, run `gofmt`/`vet`/`test`. NEVER commit/push unless the user explicitly asks (AGENTS.md requires annotated tag `vX.Y.Z` + `git push origin main --follow-tags`).
