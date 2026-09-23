# @rikipurpur/puru-ai (npm)

CLI resmi PURU-AI — **tanpa web server** secara default.
Binary Go di-download otomatis dari GitHub Releases saat install.

```bash
npm i -g @rikipurpur/puru-ai
puru setup        # wizard → tulis ~/.puru/config.json
puru gateway      # jalankan bot Telegram (long-polling, tanpa /health)
puru gateway --health --port 8080   # opt-in health check (Docker/VPS)
puru chat "halo"  # debug lokal tanpa Telegram
```

Non-interaktif (CI):

```bash
TELEGRAM_BOT_TOKEN=x PURU_BASE_URL=... PURU_API_KEY=... PURU_MODEL=puru \
  puru setup --force
```

Env installer: `PURU_AI_VERSION` (default = versi package), `PURU_AI_REPO`
(default `purujawa06-bot/PURU-AI`), `PURU_AI_BINARY` (pakai binary lokal),
`PURU_AI_SKIP_DOWNLOAD=1` (skip download).

## Termux (Android)

HP 64-bit (umum) pakai binary `puru-linux-arm64`, HP 32-bit pakai
`puru-linux-arm` (armv7) — installer otomatis pilih yang benar.

```bash
pkg install nodejs
npm i -g @rikipurpur/puru-ai
puru setup
puru gateway
```

Tips: jalankan di bawah `termux-wake-lock` agar bot tidak mati saat layar
mati. Tanpa Node pun bisa — download binary langsung dari
[Releases](https://github.com/purujawa06-bot/PURU-AI/releases)
(`puru-linux-arm64` atau `puru-linux-arm`), `chmod +x`, lalu
`./puru-linux-arm64 setup`.
