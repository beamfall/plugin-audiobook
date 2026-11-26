## 1. Plugin

- [x] 1.1 Implement `ParseDirectory`, `disc` tag support, author-prefix/noise-suffix cleaning, conflicting album detection, natural filename ordering, partial-tag `ParseWithContext`, and `PartMarkerTitle` cleaned fallback in `parse.go`
- [x] 1.2 Add tests in `parse_test.go`, `parse_directory_test.go`, and `conformance_test.go` for CORE-3480 and CORE-3481 cases
- [x] 1.3 Run `openspec validate --all --strict`, `script/gate.sh`, `script/gate.sh race`, and `codex exec --sandbox read-only` review
