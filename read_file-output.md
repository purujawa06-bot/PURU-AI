# read_file output

> Read text file; line-numbered output `LINE|content`, 1-based. Partial reads via start_line/length (default 200, max 2000).

## args

```json
{
  "path": "hello.txt"
}
```

## output

```
[file: hello.txt | total: 3 lines | read: lines 1-3]
[END OF FILE - no further content.]

1|hello world
2|line two
3|line three
```
