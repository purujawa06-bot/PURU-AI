# PURU-AI



A lightweight, self-hosted AI assistant for Telegram, built with Go.

PURU-AI combines an OpenAI-compatible model with a local tool-calling agent, persistent memory, controlled command execution, and workspace-aware file operations. It is designed to stay simple, fast, and practical to run on a small server or local machine.

## Highlights

- 🤖 **OpenAI-compatible AI agent** — use any compatible API endpoint and model.
- 🛠️ **Tool calling** — file operations, command execution, web search/fetch, Telegram utilities, environment information, and more.
- 👀 **Live tool preview** — optionally shows the tools being executed in real time.
- 🧠 **Persistent memory** — conversation history, automatic summarization, and long-term facts through `MEMORY.md`.
- 🔄 **Long-running tasks** — blocking and background command execution with session management, polling, output reading, and process control.
- 🔒 **Workspace restrictions** — optionally prevent file and command operations from escaping the configured workspace.
- 💾 **Memory-conscious runtime** — configurable Go memory limits and bounded tool output.
- ❤️ **Health endpoint** — lightweight `/health` endpoint for container liveness/readiness checks.
- 🐳 **Docker & GHCR** — ready to build and deploy as a container.
- ⚡ **Single Go binary** — no Node.js runtime or large application framework required.

## Architecture

PURU-AI is intentionally built around a small runtime:

```
Telegram
   │
   ▼
PURU-AI
   │
   ├── OpenAI-compatible model
   │
   ├── Agent / tool calling
   │      ├── File tools
   │      ├── Exec sessions
   │      ├── Web tools
   │      └── Telegram tools
   │
   ├── Conversation history
   │      └── Automatic summarization
   │
   └── Persistent memory
          └── MEMORY.md
```

The application keeps the model provider separate from local execution. Tools run in the configured workspace, while the model handles reasoning and tool selection.

## Features

### Agent tools

The agent provides tools for:

- Reading, writing, editing, and appending files
- Listing directories
- Running commands
- Managing background execution sessions
- Sending workspace files through Telegram
- Looking up Telegram user information
- Reading runtime/environment information
- Searching the web
- Fetching public HTTP/HTTPS pages

File reads are bounded, command output is capped, and workspace restrictions can be enabled through configuration.

### Command execution

The `exec` tool supports:

- Blocking commands
- Background sessions
- Session listing
- Polling
- Output reading
- Killing running sessions
- Configurable timeouts
- Memory limits
- Bounded output

Default command timeout is 60 seconds, with a configurable maximum of 300 seconds.

### Conversation memory

Conversation history is stored per chat under:

```
~/.puru/history/
```

When the configured history limit is reached, PURU-AI summarizes the conversation into Markdown context files and injects the latest summary into the system prompt.

Long-term user facts are stored separately:

```
<workspace>/MEMORY.md
```

This file is intended for durable information such as preferences and other facts that should survive conversation compaction.

## Configuration

Create your configuration from the example:

```bash
cp example.config.json ~/.puru/config.json
```

Example:

```json
{
  "telegram_bot_token": "123456:ABCDEF",
  "telegram_allowed_users": [],
  "model": {
    "base_url": "https://example.com/v1",
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
  "loop_delay_seconds": 3,
  "exec_memory_mb": 64
}
```

Configuration resolution order:

1. `--config`
2. `PURU_CONFIG`
3. `~/.puru/config.json`

Set `telegram_allowed_users` to restrict access to specific Telegram user IDs. An empty array allows all users.

## Running locally

Start the Telegram bot:

```bash
go run . --config ~/.puru/config.json
```

Run the CLI interface:

```bash
go run ./cmd/cli "hello"
```

Start the CLI in REPL mode:

```bash
go run ./cmd/cli
```

Reset CLI conversation history:

```bash
go run ./cmd/cli --reset
```

## Telegram commands

| Command | Description |
|---|---|
| `/help` | Show available commands |
| `/clear` | Clear conversation history |
| `/token` | Show context/token usage |
| `/stop` | Stop the currently running process |
| `/ai <question>` | Ask the AI from a group chat |

In private chats, normal text messages are processed directly. In groups and supergroups, the bot responds to `/ai <question>` while built-in commands remain available.

## Docker

Build the image:

```bash
docker build -t puru-ai .
```

Run it with persistent data:

```bash
docker run -d \
  --name puru-ai \
  -v puru-data:/root/.puru \
  puru-ai
```

The `/root/.puru` volume keeps configuration, workspace data, history, and memory persistent across container restarts.

## Container health

PURU-AI exposes:

```
GET /health
```

A healthy instance returns:

```json
{"status":"ok"}
```

The default address is `0.0.0.0:8080`.

## CI/CD

GitHub Actions publishes container images to GitHub Container Registry (GHCR).

The current workflow uses:

- `main` pushes for development builds
- `v*.*.*` tags for versioned releases
- `edge` for development images
- `latest` and version tags for release images
- `sha-<short>` tags for immutable build references

Image format:

```
ghcr.io/<owner>/puru-ai
```

## Development

Run the test suite:

```bash
go test ./...
```

Run static analysis:

```bash
go vet ./...
```

Check formatting:

```bash
gofmt -l .
```

## Project goals

PURU-AI focuses on four things:

1. **Small runtime footprint**
2. **Simple deployment**
3. **Useful local tools**
4. **Reliable long-running agent workflows**

The project intentionally avoids unnecessary infrastructure so it can remain easy to build, deploy, inspect, and maintain.

## License

MIT
