# append_file output

> Append content to end of file; creates it when absent. JSON escaping applies: \n newline, \\n literal backslash-n.

## args

```json
{
  "content": "\nappended",
  "path": "appendme.txt"
}
```

## output

```
Appended to appendme.txt
```
