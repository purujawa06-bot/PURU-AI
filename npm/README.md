# @rikipurpur/puru-ai

PURU-AI on npm — a lightweight, self-hosted Telegram AI assistant. No web
server by default. The Go binary is downloaded automatically from GitHub
Releases during install.

## Quick start

```bash
npm i -g @rikipurpur/puru-ai
puru setup        # interactive wizard → writes ~/.puru/config.json
puru gateway      # run the Telegram bot (long-polling, no /health endpoint)
puru gateway --health --port 8080   # opt-in health check for Docker/VPS
puru chat "hello" # local debugging without Telegram
```

## Non-interactive setup (CI)

```bash
TELEGRAM_BOT_TOKEN=x PURU_BASE_URL=https://api.openai.com/v1 \
  PURU_API_KEY=sk-... PURU_MODEL=gpt-4o-mini \
  puru setup --force
```

Installer environment variables: `PURU_AI_VERSION` (defaults to the package
version), `PURU_AI_REPO` (defaults to `purujawa06-bot/PURU-AI`),
`PURU_AI_BINARY` (use a local binary instead of downloading),
`PURU_AI_SKIP_DOWNLOAD=1` (skip the download step).

## Termux (Android)

Most 64-bit phones use the `puru-linux-arm64` binary; 32-bit phones use
`puru-linux-arm` (armv7) — the installer picks the right one automatically.

```bash
pkg install nodejs
npm i -g @rikipurpur/puru-ai
puru setup
puru gateway
```

Tip: run under `termux-wake-lock` so the bot stays alive when the screen is
off. Node.js is optional — you can download the binary straight from
[Releases](https://github.com/purujawa06-bot/PURU-AI/releases)
(`puru-linux-arm64` or `puru-linux-arm`), `chmod +x` it, then run
`./puru-linux-arm64 setup`.

## Links

- Repository: <https://github.com/purujawa06-bot/PURU-AI>
- Issues: <https://github.com/purujawa06-bot/PURU-AI/issues>
- License: MIT
