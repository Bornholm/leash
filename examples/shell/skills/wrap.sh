#!/bin/sh
: <<'SKILL'
name: wrap
description: Wraps each stdin line between a prefix and a suffix
category: text
flags:
  - name: prefix
    short: p
    default: "["
    description: String to prepend to each line
    pattern: "^.{1,10}$"
  - name: suffix
    short: s
    default: "]"
    description: String to append to each line
    pattern: "^.{1,10}$"
examples:
  - title: Default brackets
    command: echo "hello" | wrap
  - title: Custom delimiters
    command: printf 'a\nb\nc\n' | wrap --prefix="<" --suffix=">"
SKILL

while IFS= read -r line; do
    printf '%s%s%s\n' "$LEASH_FLAG_PREFIX" "$line" "$LEASH_FLAG_SUFFIX"
done
