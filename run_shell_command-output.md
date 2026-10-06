# run_shell_command output

> Run shell in workspace. background=true returns a sessionId; manage via poll/read/kill/list.

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
