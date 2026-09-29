---
name: find-skills
description: Discover and install new skills from the PuruBoy agent-tools registry. Use when no installed skill fits the task, when the user asks for a capability you do not have, or when you need a domain workflow (design, docs, code review, data) that is not covered by the installed skill catalog.
---

# Find Skills

Search the PuruBoy agent-tools registry for installable skills, then install the matching one into this workspace.

## When to Use

Use this skill when the task needs specialized knowledge or a workflow that no installed skill covers. Check the `<skills>` catalog in the system prompt first; if nothing fits, search the registry.

## Search

Use the web_fetch tool (URL-encode the query, `+` for spaces):

```
https://puruboy-api.vercel.app/api/agent-tools/find-skills?query=<keywords>&limit=5
```

Replace `<keywords>` with short task keywords (for example `web+design`). The JSON response lists candidate skills with `name`, `source`, and `skill` fields.

Pick the candidate whose description best matches the task. If none matches, answer from your own knowledge instead of forcing an install.

## Install

Fetch the chosen skill with the web_fetch tool:

```
https://puruboy-api.vercel.app/api/agent-tools/install-skills?source=<source>&skill=<skill>
```

Example:

```
https://puruboy-api.vercel.app/api/agent-tools/install-skills?source=vercel-labs/agent-skills&skill=web-design-guidelines
```

Save the returned markdown to `skills/<skill>/SKILL.md` with the write_file tool, then verify with list_dir and read_file.

Fallback: only when web_fetch is unavailable, the same URLs may be fetched via `exec` with `curl -X GET "<url>"` (curl is not guaranteed in every environment).

## Rules

- Never invent skill content: always install from the API response, then read the installed SKILL.md before applying it.
- Never overwrite an existing `skills/<name>/` directory without explicit user approval.
- After installing, read the installed SKILL.md and follow it.
