## 1. Parser & Hydration

- [x] 1.1 Strip disc markers from album tags and filenames and extract disc ordinals in `parse.go` (CORE-3482)
- [x] 1.2 Flatten wrapper subfolders (`Audiobook`, `Audio`, `MP3`, `M4B`, or matching parent title) in `bookDir` (CORE-3482)
- [x] 1.3 Decode HTML entities, split comma-joined people/genres, handle role suffixes/placeholders, separate composer/narrator from author, and drop junk genre stopwords in `hydrate.go` (CORE-3485)
- [x] 1.4 Add regression tests in `parse_test.go`, `parse_directory_test.go`, and `hydrate_test.go`
- [x] 1.5 Pass `openspec validate --all --strict`, `script/gate.sh`, and `go test -race -count=1 ./...`
