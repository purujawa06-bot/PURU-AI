# run_shell_command output

> Run or manage shell commands on the host. Use action=run to execute (set background=true for long commands, then poll with action=poll and the returned sessionId). action=list shows sessions, action=read reads output, action=kill stops a running session. Commands run with cwd inside the workspace unless overridden.

## args

```json
{
  "action": "run",
  "command": "echo hi",
  "timeout": 30
}
```

## output

```
{
  "success": true,
  "exit_code": 0,
  "output": "hi\n"
}
```
