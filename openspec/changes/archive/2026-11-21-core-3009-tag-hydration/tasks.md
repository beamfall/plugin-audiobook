## Implementation
- [x] Export the bundle vocabulary constants and declare the `audiobook-hydrated` flag field
- [x] Add `MapHydration` and its plan types, reusing the parser's series-folder rule
- [x] Table-driven tests for tag precedence, splitting, role merge, series sequence and genre
- [x] `script/gate.sh`, tag the release (v0.1.16), then `openspec archive core-3009-tag-hydration`
