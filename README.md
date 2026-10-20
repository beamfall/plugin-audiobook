<p align="center">
  <img src=".github/brand/beamfall-logo.png" alt="Beamfall" width="96" height="96">
</p>

# Beamfall Audiobooks

Beamfall Audiobooks is the audiobook plugin for [Beamfall Core](https://github.com/beamfall/core).
Point a library at your `.m4b` and `.mp3` files and each book becomes one item, however many
files it is split into, with its chapters in order, its author, series and narrator filled in,
and a single resume position across the whole book. It can also import listening progress from
Audiobookshelf.

Like the rest of Beamfall, it is free and open source, needs no account, and sends no telemetry.

## Features

- A multi-file book is folded into a single title with its files as
  ordered parts, and each part becomes a chapter you can jump to. Chapter labels come from each
  file's title tag, or from the file name with its track number stripped.
- Book title and part order are read from embedded tags
  (`album`, `track`). When tags are missing, the plugin understands the common
  `Author/Series 03/Title/` folder layout and numbered file names like `01 - Chapter One.mp3`.
  It will not merge an untagged folder of several files into one book on guesswork.
- Series positions are kept as text, so a "Book 4.5" novella sorts between books 4 and 5. Folder names like `Mistborn 2`, `Series #4` and
  `Series Book 2` are recognized.
- The narrator comes from the `narrator` tag, then `composer`. The
  `artist` tag is deliberately ignored, because in most rips it names the author.
- Authors, narrators and translators share a single Person entry with a role on each credit, so
  an author who narrates their own book has one entry.
- Audiobooks keep a resume position, and the home screen leads with
  Continue Listening, then Recently Added, Series, Authors and All Books.
- Titles, summaries, release years, cover art, authors and genres
  can come from the iTunes Search API and Open Library, which need no key, or from Google Books
  with an API key you supply. iTunes and Open Library requests are paced to each service's
  rate guidance.
- The Audiobookshelf importer reads an Audiobookshelf progress
  export or server backup (up to 64 MB) and carries over playback position, duration, finished
  state and the time of the last update. A backup with progress for more than one user is refused, so
  nobody's listening history is merged into someone else's profile.
- The plugin's fields and home sections have English and French labels.

## How it fits into Beamfall

This plugin ships with [Beamfall Core](https://github.com/beamfall/core), the self-hosted media
server; you do not install it separately. Core owns scanning, playback, storage and the apps;
this plugin contributes the audiobook-specific parts: the data model, the home layout, the
metadata sources, the file and tag parser, and the Audiobookshelf importer.

It is built on the public [Beamfall plugin SDK](https://github.com/beamfall/plugin-sdk)
(`github.com/beamfall/plugin-sdk`). The bundle is plain declarative data in `bundle.go`, and the
scan-side parser is in `parse.go`. The SDK's
[plugin-authoring guide](https://github.com/beamfall/plugin-sdk/blob/main/docs/plugin-authoring.md)
explains how plugins plug into Core.

## Develop

You need Go (see `go.mod` for the version), Python 3 and
[golangci-lint](https://golangci-lint.run/).

```sh
go test ./...          # unit and conformance tests
script/gate.sh         # gofmt, go vet, coverage floor, golangci-lint
script/gate.sh race    # tests under the race detector
script/gate.sh fuzz    # bounded fuzzing of the parser and importer
```

`conformance_test.go` runs the SDK's `sdktest` conformance harness against the parser's golden
vectors and the declared bundle. CI runs the same gate on every pull request and every push to `main`.

## Contributing

Contributions require a signed Beamfall Contributor License Agreement. Read
[CONTRIBUTING.md](CONTRIBUTING.md) and [CLA.md](CLA.md) before opening a pull request.

## Security

Please report vulnerabilities privately as described in [SECURITY.md](SECURITY.md). Do not open a
public issue.

## License

Beamfall Audiobooks is licensed under the GNU Affero General Public License, version 3. See
[LICENSE](LICENSE).
