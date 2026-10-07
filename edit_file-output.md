# edit_file output

> Replace an exact occurrence of old_string with new_string in a file. old_string must match exactly once; include surrounding context to make it unique. Use edit_file_by_line when you only know line numbers.

## args

```json
{
  "new_string": "Puru",
  "old_string": "world",
  "path": "/tmp/puru-tools-dump-2243941877/editme.txt"
}
```

## output

```
File edited: /tmp/puru-tools-dump-2243941877/editme.txt
```
