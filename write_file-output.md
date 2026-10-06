# write_file output

> Write file; creates parent dirs. overwrite=true replaces the ENTIRE file. JSON escaping applies: \n newline, \\n literal backslash-n.

## args

```json
{
  "content": "hi\nthere\n",
  "overwrite": true,
  "path": "written.txt"
}
```

## output

```
File written: written.txt
```
