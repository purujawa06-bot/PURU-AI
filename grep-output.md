# grep output

> Search file contents for a keyword; narrow by path/ext. Max matches default 50, max 200.

## args

```json
{
  "keyword": "hello",
  "path": "."
}
```

## output

```
hello.txt:1: hello world
sub/nested.txt:1: nested hello

```
