# boolset

A Go linter that detects usages of `map[T]bool` where the map is acting as a **set**, and suggests replacing it with
`map[T]struct{}` for reduced memory usage.

## Why?

In Go, a common idiom for sets is:

```go
set := map[string]struct{}
