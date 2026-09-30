---
name: puruclaw-configure
description: PuruClaw PURU-AI configuration help. Use when user asks about config, config.json, example.config.json, setup, installation, env, tokens, model, Telegram bot, workspace, or any config field.
---

# PuruClaw Configure

Answer PuruClaw config questions from the canonical example config. Never invent field names or defaults from memory.

## Source of truth

Canonical file (branch `main`):

`https://github.com/purujawa06-bot/PURU-AI/raw/refs/heads/main/example.config.json`

Fetch it on every config question — it may change on main:

```bash
curl -sL 'https://github.com/purujawa06-bot/PURU-AI/raw/refs/heads/main/example.config.json' -H 'User-Agent: Mozilla/5.0'
```

Fallback with `web_fetch` when curl is unavailable. If the fetch fails, say so and stop — do not guess config content.

## How to answer

1. Fetch the canonical JSON above.
2. Answer only with fields present in that JSON.
3. For setup questions: tell the user to copy `example.config.json` to `config.json` and fill in secrets (`telegram_bot_token`, model `api_key`, `web_search.aistudio.api_key` when used).
4. Never print real secrets. Use placeholders from the example file (`123456:ABCDEF-replace-with-bot-token`, `sk-replace-with-openai-key`).
5. Explain unknown fields as "not in the canonical example config" instead of inventing them.

## Field guide (verify against fetched JSON)

- `telegram_bot_token`: BotFather token for the Telegram channel.
- `telegram_allowed_users`: allowlist of Telegram user IDs, empty = allow all (restrict in production).
- `model.base_url`, `model.api_key`, `model.model`, `model.temperature`: OpenAI-compatible chat model settings.
- `workspace`, `restrict_workspace`: workspace path and path restriction flag.
- `max_iterations`, `history_token_limit`, `loop_delay_seconds`, `exec_memory_mb`: agent loop limits.
- `host`, `port`: HTTP listen address.
- `tools_preview`, `skills_mode` (`default|off|custom`), `skills_allow`, `timezone`: runtime/skills options.
- `web_search.aistudio.active`, `web_search.aistudio.model`, `web_search.aistudio.api_key`: optional Gemini fallback for web search.
