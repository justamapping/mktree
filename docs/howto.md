# mktree howto

mktree creates directories and files from an indented tree. Write the tree you want, pipe it in, done.

```sh
mktree <<'EOF'
project/
  src/main.go
    package main

    func main() {}
  data/
  .gitignore
EOF
```

This creates `project/src/main.go` with that content, plus an empty `project/data/` and an empty `project/.gitignore`, all inside the current directory. Pass a directory as the last argument to apply somewhere else: `mktree <<'EOF' ~/code`. Use `<<'EOF'` with quotes so the shell leaves `$` and backticks in file contents alone.

## Format

```tree
app/                    # a trailing / makes a directory
  cmd/server/main.go    # a/b/c shorthand creates the parents
  internal/
    store/
  Makefile
    build:
    	go build ./...
  README.md
    # App

    Lines under a file are its content, kept exactly as written.
  .env                  # no content: created empty, or left alone if it exists
```

- **Directories** end with `/`. Anything else is a file.
- **Indent with spaces.** The first indented line sets the unit (2 or 4 is typical), and every level is one unit deeper. A tab in tree indentation is an error.
- **Content** is every line indented one unit deeper than its file, until a line comes back to the file's level or shallower. One unit of indentation is stripped and the rest is kept as-is: `#`, tabs, extra indentation, `├──` diagrams. Makefile recipes are written as indent + a real tab.
- Blank lines inside content are kept. Trailing blank lines are dropped, and every file with content ends in exactly one newline.
- **Comments** begin with ` #` on tree lines, or `#` at the start of a line between nodes. Inside content, `#` is just text.
- **Quote** names that contain ` #` or have leading or trailing spaces: `"odd # name.txt"`, `" padded/"`.
- Repeating a directory merges it, so `src/a.go` and `src/b.go` share one `src/`. Repeating a file is an error.
- Box-drawing prefixes (`├──`, `└──`, `│`) count as indentation, so tree diagrams work as input as long as directories end with `/`, the way `tree -F` prints them.

## What applying does

mktree plans first, then writes. It **never deletes** anything.

| tree says                 | disk has                  | result                              |
|---------------------------|---------------------------|-------------------------------------|
| anything                  | nothing                   | `+` created                         |
| directory                 | directory                 | `=` kept, children checked          |
| file without content      | file                      | `=` kept, contents untouched        |
| file with content         | same content              | `=` kept                            |
| file with content         | different content         | `!` conflict (`--force`: `~` overwritten) |
| file or directory         | the other kind, a symlink | `!` conflict, even with `--force`   |

If anything conflicts, mktree prints the conflicts, writes **nothing**, and exits 1. It's safe to re-run the same tree: anything already done shows as `=`. Files on disk that aren't in the tree are ignored, so you can add new files to an existing project by applying a tree that lists only them, or the whole layout.

Output lists `+` and `~` and `!` lines. `-v` adds `=` lines, `-q` shows only conflicts.

Exit codes: `0` ok, `1` conflicts or an I/O error, `2` the input or flags are invalid. Parse errors read `file:line:col: message`, with `<stdin>` as the file for piped input.

## Checking before you write

```sh
mktree -n < project.tree          # print the plan, write nothing
mktree convert < project.tree     # show how the tree parses, normalized
mktree convert --to json < project.tree   # see exact file contents
```

## Other commands

```sh
mktree dump [DIR]                 # print an existing directory as a tree
mktree dump --contents [DIR]      # include the content of text files (up to 64 KiB)
mktree convert --from json --to yaml < tree.json
```

`dump` skips dotfiles (`--hidden` includes them) and never includes `.git`. Use `-I 'node_modules'` (repeatable, glob patterns) or `--gitignore` to skip more. Symlinks are skipped.

## JSON and YAML

Use them when you need exact bytes, such as a file without a trailing newline, trailing blank lines, or lines that are only whitespace. The shape is the same in both:

```json
[
  {"name": "app", "type": "dir", "children": [
    {"name": "main.go", "type": "file", "content": "package main\n"},
    {"name": ".env", "type": "file"}
  ]}
]
```

`content` omitted means "create empty or leave alone". `"content": ""` means "must be empty". The format is picked by file extension with `-f`, or set with `--from json` or `--from yaml`.

## Limits

- No deletion, no renames, no permissions (files are `0644`, directories `0755`), no symlinks.
- The target directory must already exist. The tree can create everything below it.
- Flags go before the target: `mktree -n ~/code`, not `mktree ~/code -n`.
- The first indented line in a document sets the unit, so it can't be file content that itself starts with extra indentation. This only comes up when a top-level file is listed first. Put a directory first, or use JSON.
