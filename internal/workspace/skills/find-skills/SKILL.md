---
name: find-skills
description: Discover and install new skills from the skills.sh directory. Use when no installed skill fits the task, when the user asks for a capability you do not have, or when you need a domain workflow (design, docs, code review, data) that is not covered by the installed skill catalog.
---

# Find Skills

Search the skills.sh directory for installable skills, then install the matching one into this workspace.

## When to Use

Use this skill when the task needs specialized knowledge or a workflow that no installed skill covers. Check the `<skills>` catalog in the system prompt first; if nothing fits, search the directory.

## Search

Use `run_shell_command` with curl (preferred). `web_fetch` works as fallback but curl handles compression better.

```bash
curl 'https://www.skills.sh/api/search?q=<keywords>&limit=10' \
  -H 'User-Agent: Mozilla/5.0 (Linux; Android 10; RMX2185 Build/QP1A.190711.020) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/153.0.8010.36 Mobile Safari/537.36' \
  -H 'Referer: https://www.skills.sh/?q=ru' \
  --compressed
```

Replace `<keywords>` with short task keywords, URL-encoded (`+` or `%20` for spaces, e.g. `q=code+review`).

Response JSON shape:

```json
{"query":"...","skills":[{"id":"owner/repo/skill-id","source":"owner/repo","skillId":"skill-id","name":"skill-id","installs":123}],"count":10}
```

Notes:
- Search is public, no auth needed. Do NOT use `/api/v1/...` endpoints, those require a Vercel OIDC token.
- Search results carry no description. Open the detail page to confirm fit:
  `https://www.skills.sh/<source>/<skillId>`
  Example: `https://www.skills.sh/vercel-labs/agent-skills/web-design-guidelines`
- Detail page shows description, install count, and the canonical install command:
  `npx skills add https://github.com/<source> --skill <skillId>`

Pick the candidate whose name best matches the task, preferring higher `installs`. If none matches, answer from your own knowledge instead of forcing an install.

## Install

Prefer manual fetch via GitHub raw (works on minimal hosts without npm). Try `main` branch first, then `master`.

Try in order for `<source>` = `owner/repo`, `<skill>` = skillId:

1. `https://raw.githubusercontent.com/<source>/main/skills/<skill>/SKILL.md`
2. `https://raw.githubusercontent.com/<source>/main/<skill>/SKILL.md`
3. Nested repos (e.g. `mattpocock/skills` groups by category like `skills/engineering/<skill>/`): list the parent via GitHub API, then descend:
   `https://api.github.com/repos/<source>/contents/skills?ref=main`
   Find the dir named `<skill>`, possibly one level deeper, then fetch its `SKILL.md`.

Discovery helper via GitHub API (no token needed for public repos, rate-limited):

```bash
curl -s 'https://api.github.com/repos/<source>/contents/skills/<skill>?ref=main' -H 'User-Agent: Mozilla/5.0'
```

- If it returns a file list containing `SKILL.md`, fetch `download_url` for it.
- If `{"message":"Not Found"}`, list `.../contents/skills?ref=main` and look one level deeper.

Example (vercel-labs):

```bash
curl -s 'https://raw.githubusercontent.com/vercel-labs/agent-skills/main/skills/web-design-guidelines/SKILL.md' \
  -H 'User-Agent: Mozilla/5.0' | head -c 2000
```

Save the returned markdown to `skills/<skill>/SKILL.md` with the write_file tool, then verify with list_dir and read_file.

If the fetched SKILL.md references sibling `references/...`, `scripts/...`, or `assets/...` files, fetch them from the same raw base path and save preserving relative paths.

Fallback: only when raw fetch fails and npm exists, run via `run_shell_command`:
`npx -y skills add https://github.com/<source> --skill <skill>`
then copy the resulting SKILL.md into workspace `skills/<skill>/SKILL.md`.

## Rules

- Never invent skill content: always fetch from GitHub raw, then read the installed SKILL.md before applying it.
- Never overwrite an existing `skills/<name>/` directory without explicit user approval.
- After installing, read the installed SKILL.md and follow it.
- Always send the mobile User-Agent + Referer headers on skills.sh API calls.
