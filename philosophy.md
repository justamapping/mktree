# Mktree

mktree is a tool for scaffolding directories with ease

## idea

you simply declare a tree structure for your directories, and it'll output the AST in any language

Example:
```mktree
project/
  backend/                  # example backend
    docs/
    internal/
      flow/
      parse/
      store/                # the db
      api/                  # http handlers, generated from spec
  web/                      # nextjs static export
  api/
    openapi.yaml            # the contract between backend and web
  deploy/
    docker-compose.yml      # docker stuff
    Dockerfile
  docs/
    architecture.md
      hello world!
  Makefile
```

This scaffolds the structure exactly as it looks

files can indent from a directory, file contents can indent from a file

This literally is an AST in a very basic form, so you can keep it as is, or output it to json or yaml

It can be used either as a command for agents to scaffold this quickly, or it can be derived from a file

i.e.
```mktree -f myproject.tree```
```mktree -f myproject.json```
```mktree "{your tree here}"```
```mktree -a -f project.tree . # append a tree to your current directory ```

## features

we can do this incrementally to a directory that already exists, we shouldn't be overwriting or deleting things at large however
