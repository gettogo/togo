```mermaid
flowchart LR
  Cursor[Cursor IDE] --> hooksJson[".cursor/hooks.json"]
  hooksJson --> dumpSh["dump.sh"]
  dumpSh --> hooksLog["hooks.log"]
```
