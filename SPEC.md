# mktree spec (draft)

## 1. Tree format

A `.tree` document is a list of lines. Each line is either a **tree line** (a directory or file) or a **content line** (part of a file body).

### 1.1 Nodes

```
name/        directory
name         file
a/b/c/       directory, intermediate directories created
a/b/file.go  file, intermediate directories created
```

- A trailing `/` marks a directory. Anything else is a file.
- Top-level lines are created relative to the target directory. Multiple top-level nodes are allowed.
- Names must not be empty, `.`, `..`, contain a `..` segment, or start with `/`. These are parse errors.
- Declaring the same directory twice merges them, so `src/a.go` and `src/b.go` share one `src`. Declaring a file twice, or a file and a directory with the same name, is a parse error.

### 1.2 Indentation

- Structure is indented with spaces only. A tab in tree-line indentation is a parse error (same rule as YAML).
- The **indent unit** is the indentation of the first indented line in the document (usually 2 or 4).
- A child is indented exactly one unit deeper than its parent. Tree lines must sit at a multiple of the unit.
- Only directories can have child nodes. A line indented under a file is content (1.4).

### 1.3 Comments

Comments exist only on tree lines, never inside file bodies.

```
store/          # the db
# full-line comment
```

- An inline comment begins at `#` preceded by at least one space.
- A line whose first non-space character is `#` is a full-line comment, unless it is inside a file body.
- Quote a name to include ` #` or leading/trailing spaces: `"weird # name"`, `"my dir/"`.
  Quotes wrap the whole path, and `/` keeps its meaning inside them. Inside quotes, `\"` and `\\` are the only escapes.

### 1.4 File bodies

Bodies use indentation as the delimiter, the same model as a YAML literal block scalar (`|`), with no indicator.

```
docs/
  architecture.md
    # Architecture        <- content, not a comment
      indented line       <- extra indentation preserved

  Makefile
```

For a file at indent `n`, the **content prefix** is `n + unit` spaces.

- The body is every line after the file line until the first non-blank line indented `n` or less.
- Each body line has exactly the content prefix removed. Everything after the prefix is raw bytes, including `#`, tabs, trailing `/`, and box-drawing characters. That means Makefile recipes work as `<prefix>\techo hi`.
- A non-blank line indented more than `n` but less than the prefix is a parse error.
- Whitespace-only lines inside a body are empty lines, whatever their length, because editors strip trailing whitespace.
- Trailing blank lines are dropped and the file ends with exactly one `\n`. This matches YAML's default "clip" chomping. Use JSON/YAML input when exact bytes matter (no trailing newline, binary).

A file with no body means *ensure it exists*. A file with a body means *ensure it has this content*. See 3.

### 1.5 Blank lines

Blank lines outside bodies are ignored. A blank line between a file and the next sibling ends nothing on its own. Indentation decides.

### 1.6 `tree`-style input

Box-drawing prefixes as emitted by `tree` (and by most LLMs) are accepted:

```
project/
├── backend/
│   └── store/
└── Makefile
```

Before measuring indentation, each of `├`, `└`, `│`, `─` and non-breaking space (which some `tree` versions emit) in a line's leading prefix counts as one column. With `tree`'s 4-column layout, the unit becomes 4. Between tree lines, a line containing only `│` and whitespace is blank.

Inside a body, only the content prefix is stripped, so a README that contains a tree diagram keeps it intact.

## 2. Data model (JSON / YAML)

The AST is a list of nodes:

```json
[
  {
    "name": "project",
    "type": "dir",
    "comment": "optional",
    "children": [
      { "name": "Makefile", "type": "file" },
      { "name": "architecture.md", "type": "file", "content": "hello world!\n" }
    ]
  }
]
```

- `type`: `"dir"` or `"file"`.
- `children`: dirs only. Omitted means empty.
- `content`: files only. Omitted means "ensure exists". `""` means "ensure empty". The string is written exactly as given, with no chomping.
- `comment`: optional, preserved on round-trip through `.tree`.
- YAML uses the same shape.
- `a/b/` shorthand is expanded into nested nodes in the AST.

Round-trip guarantee: `tree → AST → tree` is stable after one normalization pass (canonical unit of 2, shorthand expanded, comments two spaces after the name).

`AST → tree` is lossy for content in the same ways parsing is (trailing newline clipped, whitespace-only lines emptied). One case can't be expressed at all: when the document's first indented line would be a body line that itself starts with indentation, because the parser would infer the wrong unit. Formatting fails with an error pointing to JSON/YAML instead. An `--indent` override would remove this case.

## 3. Apply semantics

mktree never deletes. It plans first and writes only if the plan has no conflicts.

| tree says            | disk has               | result                                |
|----------------------|------------------------|---------------------------------------|
| dir                  | nothing                | create                                |
| dir                  | dir                    | no-op, recurse                        |
| dir                  | file                   | **conflict**                          |
| file, no body        | nothing                | create empty                          |
| file, no body        | file                   | no-op (contents untouched)            |
| file, body           | nothing                | create with body                      |
| file, body           | file, same bytes       | no-op                                 |
| file, body           | file, different bytes  | **conflict** (`--force`: overwrite, `--no-clobber`: keep) |
| file                 | dir                    | **conflict** (never forced)           |

- If any conflicts exist, report them all and exit 1 without writing. `--force` and `--no-clobber` (mutually exclusive) resolve only the content conflict, by overwriting or keeping the file on disk. `--no-clobber` is for files meant to diverge, such as per-machine config that a repo can't track.
- Symlinks on disk are not followed during apply. A symlink in the path is a conflict. Other non-regular files (fifos, sockets, devices) are conflicts too, and are never read.
- Children of a conflicting directory are not planned.
- The target directory must already exist. It can be a symlink, since the user named it.
- New files are created exclusively, so a file that appears between planning and writing causes an error instead of being clobbered. An I/O error partway through leaves the steps completed so far in place, which is safe because nothing is deleted and rerunning picks up where it stopped.
- New files get mode `0644` and new directories `0755`, both subject to umask.

## 4. Dump

`mktree dump [DIR]` walks a directory and emits a tree.

- Default output is the `.tree` format with a 2-space unit and no contents.
- `--contents`: include bodies for text files up to `--max-size` (default 64 KiB). Binary and oversized files are emitted with no body.
- `.git` is never included. `--hidden` includes dotfiles, which are excluded by default the same way `tree` does. `-I PATTERN` skips base names matching a shell glob (repeatable).
- `--gitignore` skips what `git ls-files --others --ignored --exclude-standard` reports, so it needs git and a repository.
- Entries are sorted by name, directories first.
- Symlinks and other non-regular files are skipped, since symlinks are out of scope.
- In tree output, a body the format would change is omitted with a warning on stderr. That covers a missing trailing newline, trailing blank lines, whitespace-only lines, CRLF, and a first line that starts with indentation. A dump never misstates a file. JSON and YAML keep exact bytes.

Invariant: `mktree dump D --contents --to F | mktree -n --from F D` reports no changes, for every format F.

## 5. CLI

```
mktree [flags] [TARGET]            apply a tree (TARGET defaults to .)
mktree dump [flags] [DIR]          directory → tree on stdout
mktree convert [flags]             tree ⇄ json ⇄ yaml, no filesystem access
```

Input for `apply`/`convert` comes from `-f FILE`, or from stdin if `-f` is omitted or `-f -`:

```sh
mktree -f project.tree
mktree -f project.json ~/code
mktree <<'EOF'
project/
  src/
    main.go
EOF
```

| flag              | meaning                                                |
|-------------------|--------------------------------------------------------|
| `-f FILE`         | input file (`-` for stdin)                             |
| `--from FMT`      | input format: `tree`, `json`, `yaml` (default: by extension, else `tree`) |
| `--to FMT`        | output format for `dump` / `convert` (default `tree`)  |
| `-n, --dry-run`   | print the plan, write nothing                          |
| `--force`         | overwrite files whose content differs                  |
| `--no-clobber`    | keep files whose content differs, reported as `=` with a note |
| `-q, -v`          | quiet / verbose                                        |
| `--howto`         | print the full guide (`docs/howto.md`, embedded in the binary) for humans and agents |

Flags go before `TARGET`/`DIR` (standard Go flag parsing). A target directory literally named `dump` or `convert` is written `./dump`.

Plan output, one line per node. Creates, overwrites and conflicts are printed by default. `-v` adds existing entries, and `-q` prints only conflicts. The plan is the same with and without `-n`:

```
+ project/backend/store/
+ project/Makefile
= project/docs/
! project/deploy/Dockerfile   content differs
```

`+` create, `=` exists, `~` overwrite (forced), `!` conflict.

Exit codes: `0` success, `1` conflicts or I/O error, `2` parse or usage error. Parse errors are reported as `file:line:col: message`, with `<stdin>` as the file name for stdin. JSON and YAML input reject unknown fields and go through the same validation as tree input.

The `-a` flag from the original idea is dropped because merging is the only mode. The inline-string form (`mktree "{tree}"`) is replaced by stdin/heredoc, which avoids shell-quoting problems with multi-line input.

## 6. Open questions

- Executable bit: support an `ls -F`-style `run.sh*` suffix?
  no
- Symlinks: add a `link -> target` syntax, or keep them out of scope?
  out of scope
- Should the indent unit be overridable (`--indent N`) for documents whose first indented line is file content?
  not sure
- Should `dump` show existing comments? Comments could live in a sidecar file, but that's probably out of scope.
  no
