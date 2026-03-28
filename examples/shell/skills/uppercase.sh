#!/bin/sh
: <<'SKILL'
name: uppercase
description: Converts each stdin line to uppercase
category: text
flags:
  - name: prefix
    short: p
    default: ""
    description: Prefix to prepend to each line
examples:
  - title: Simple conversion
    command: echo "hello world" | uppercase
  - title: With prefix
    command: echo "hello world" | uppercase --prefix=">> "
SKILL

while IFS= read -r line; do
    printf '%s%s\n' "$LEASH_FLAG_PREFIX" "$(printf '%s' "$line" | tr 'a-z' 'A-Z')"
done
