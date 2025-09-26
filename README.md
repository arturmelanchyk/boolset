# boolset

`boolset` is a Go linter that finds `map[T]bool` values which are effectively used as sets and recommends switching to
`map[T]struct{}`. The replacement avoids storing redundant boolean payloads and can cut memory usage for large maps in
half while keeping semantics identical.

## Why it matters

The canonical set idiom in Go is to use a map whose values are the zero-sized struct:

```go
set := map[string]struct{}{"a": {}, "b": {}}
```

Using `map[T]bool` instead means every entry carries a full boolean value. When all stored values are `true`, that extra
byte is wasted on every element, and the intent of “membership only” is less clear to readers. `boolset` pinpoints those
cases so teams can standardise on the more efficient pattern.

## How the linter decides

`boolset` performs a full type-check of the package and then walks the syntax tree to discover assignments into
`map[T]bool` values. A warning is only emitted when the linter can *prove* that every assignment stores `true`.

The analyser currently recognises the following as evidence of `true`-only usage:

- Explicit literals, including tuples such as `m[a], m[b] = true, true`.
- Composite literals, e.g. `map[string]bool{"a": true}`.
- Constant-folded expressions that evaluate to `true` (`1 < 2`, `!false`, etc.).
- Local boolean variables that are provably always `true` within their scope, even when written through aliases.
- Predeclared or constant identifiers that resolve to the literal `true`.
- Type aliases whose underlying type is `map[T]bool`.
- Loops that repeatedly store `true` into the same map.

Assignments that introduce `false`, rely on user input, call results, or refer to variables that might change value keep
the map out of the warning set. Composite literals, struct fields, and method receivers are all inspected, but global
variables and fields are treated conservatively because their values might change outside the analyser’s view.

Example diagnostic:

```
path/to/file.go:12:9: map[string]bool only stores "true" values; consider map[string]struct{}
```

## Running the linter

The repository ships with a simple CLI wrapper:

```bash
go run ./cmd/boolsetlint ./...
```

The tool exits with a non-zero status if any eligible `map[T]bool` usages are found, making it easy to wire into CI or a
pre-commit hook.

Install the CLI globally with:

```bash
go install github.com/arturmelanchyk/boolset/cmd/boolsetlint@latest
```

The CLI understands Go's `...` package patterns, so paths like `./...` or `internal/...` recurse through matching
directories. Use standard shell quoting if your shell expands `...` glob patterns.

When issues are detected, `boolsetlint` prints each diagnostic and finishes with a summary line reporting the total
count, e.g. `boolsetlint found 3 issue(s)`.

### golangci-lint integration

`boolset` also ships as a golangci-lint module plugin, making it easy to wire into existing linting pipelines that rely
on golangci-lint's orchestrated runner.

1. Build the plugin shared object locally (optional if you plan to load it via module reference):

   ```bash
   go build -buildmode=plugin -o boolset.so ./plugin/golangci
   ```

   (Pick any output path you prefer; adjust the following config accordingly.)

2. Configure golangci-lint to load the plugin and enable the linter. You can either point at a locally built shared
   object or let golangci-lint fetch the module directly.

   **Local shared object**

   ```yaml
   run:
     # Ensure the runner sees your plugin; paths are resolved relative to the config file.
     plugins:
       - path: ./boolset.so

   linters:
     enable:
       - boolset
   ```

   **Module plugin fetched by golangci-lint**

   ```yaml
   version: v2.5.0
   plugins:
     - module: github.com/arturmelanchyk/boolset
       import: github.com/arturmelanchyk/boolset/plugin/golangci
       version: v0.1.0

   linters:
     enable:
       - boolset
   ```

   Replace `v0.1.0` with the release tag you want to pin to (`latest` also works during experimentation).

With that in place, `golangci-lint run` will execute the boolset analyzer alongside the other enabled linters.

## Limitations and roadmap

The linter focuses on provable `true` assignments. It does not attempt deep data-flow analysis across function
boundaries, so cases where a helper always returns `true` will not trigger unless they are constant-folded by the type
checker. Pointer indirections such as `(*setPtr)[key] = true` are currently skipped, and global variables or struct
fields are handled conservatively because their values may change outside the analyser's view. Contributions that expand
the reasoning while keeping false positives low are welcome.

## Contributing

Run the tests before opening a PR:

```bash
go test ./...
```
