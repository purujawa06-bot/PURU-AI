# read_file output

> Read a text file from the local workspace and return its contents with line numbers, optionally sliced by start_line/length. Read a file before editing it and reuse the content already in context instead of re-reading. Default limit is 200 lines.

## args

```json
{
  "path": "/tmp/puru-tools-dump-2243941877/hello.txt"
}
```

## output

```
1|hello world
2|line two
3|line three
```
