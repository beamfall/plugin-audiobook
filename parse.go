// Package audiobook is the Audiobook filename/tag parser hook: it folds a
// leaf audio folder into one book's ordered Parts and extracts
// Author/Series/Title/sequence. It implements the scan-side FilenameParser
// seam via the plugin SDK rather than importing Core's scanner directly, so it
// can ship in this independent, AGPL-licensed plugin module.
//
// Metadata is tags-primary (in practice a well-tagged .m4b carries author/title/series), with the folder/filename
// convention "Author/Series ##/Title/" as the fallback. The parser reads the
// probe's container tags (album/artist/album_artist/track) when present and
// falls back to the path layout otherwise.
//
// A tag-supplied part ordinal, including 0 for prologues, also emits
// ParsedMedia.PartGroup as the leaf folder key. Core scan consumes only that
// explicit neutral shape; parsers that do not set PartGroup keep the historical
// one-file/one-Media import behavior.
//
// # Known heuristic limitations (digit-containing titles)
//
// The bare-numeric fallbacks (reLeadingOrdinal, reSeriesFolder) are purely
// lexical and cannot distinguish digits that are a real part/series ordinal
// from digits that belong to a title or year. Concretely, when tags are absent:
//   - A leading number on a filename is read as a part ordinal, so
//     "100 Years of Solitude.mp3" gets Index=100 and "1984.m4b" gets Index=1984.
//   - A series (grandparent) folder ending in a number is read as a series
//     position, so "Live at Wembley 95/" yields Sequence="95".
//
// These are accepted false positives, NOT a design goal. No strictly-dominating
// refinement exists: any rule that suppresses them (requiring a keyword, a
// separator, or a short remainder) also suppresses the dominant, pinned
// conventions this parser must keep — bare "01 - Chapter One" chapter files and
// bare "Mistborn 2" / "Series 4.5" series folders are structurally identical to
// the false positives above. Tags (album/track) resolve the ambiguity when
// present and always win. The conformance suite pins this behavior under
// "Known heuristic limitations" so any future change to the tradeoff is
// deliberate and visible rather than a silent regression.
package audiobook

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	sdk "github.com/beamfall/plugin-sdk"
)

// PluginID must match bundle.go `ID: "audiobook"` — scan resolves the parser
// through the Library's plugin binding.
const PluginID = "audiobook"

// ClassAudiobook is the conventional `class` Media field value this plugin's
// books carry; written by the parser and implied by identification.
const ClassAudiobook = "audiobook"

// Parser implements scan.FilenameParser with stdlib-only tag/path heuristics.
type Parser struct{}

var (
	_ sdk.FilenameParser        = (*Parser)(nil)
	_ sdk.ContextFilenameParser = (*Parser)(nil)
	_ sdk.MediaFieldParser      = (*Parser)(nil)
)

// New returns a ready Parser.
func New() *Parser { return &Parser{} }

var (
	// A track ordinal at the start of a filename: "01 - ", "01.", "1 ",
	// "Track 03", "Chapter 12", "Part 2". The capture group is the number.
	// Case-insensitive ((?i)) so it matches the ORIGINAL string: callers slice
	// the input at the match offsets, and matching a strings.ToLower copy
	// instead would drift those byte offsets whenever lowering changes a
	// rune's encoded length (e.g. U+212A KELVIN SIGN → "k").
	// LIMITATION: a bare leading number is also matched on digit-titles
	// ("100 Years of Solitude", "1984") — see the package "Known heuristic
	// limitations" note before tightening this.
	reLeadingOrdinal = regexp.MustCompile(`(?i)^(?:track|chapter|part|disc|cd)?[ ._-]*(\d{1,4})(?:\b|_)`)
	// A series-position folder segment: "Series Name 03", "Series Name #4",
	// "Series Name Book 2", "Series Name BOOK 2", "Series Name 4.5". Captures
	// (name, sequence). The "book" keyword is matched case-insensitively
	// ((?i:...), scoped to that group only) so "Book"/"BOOK"/"book" all match;
	// the digit grammar and "#" form are untouched.
	// LIMITATION: also matches names ending in a year/number for non-series
	// reasons ("Live at Wembley 95") — see the package "Known heuristic
	// limitations" note before tightening this.
	reSeriesFolder = regexp.MustCompile(`^(.*?)[ -]+(?:(?i:book)[ -]*|#)?(\d{1,3}(?:\.\d{1,3})?)$`)

	cleanNameReplacer = strings.NewReplacer(".", " ", "_", " ")
)

// Parse derives a best-guess ParsedMedia for one audiobook file. Tags win when
// present (tags-primary); the folder/filename convention is the
// fallback. ok is true only when a non-empty Title was derived.
//
// Field mapping onto the domain-agnostic seam (sdk.ParsedMedia, named by
// shape):
//   - Title    — the book title (tag "album", else the leaf folder name).
//   - Class    — always "audiobook" when a title is found (single MediaClass).
//   - Sequence — the book's position in its Series (folder "Series ## /");
//     empty when the book is not part of a recognised series.
//   - Index    — this file's part ordinal within the book (track tag, else a
//     leading filename number), 0 when single-file or unknown.
//   - PartGroup — the leaf folder key for ordered Parts when Index is known.
func (p *Parser) Parse(path string, probe sdk.ParseProbe) (sdk.ParsedMedia, bool) {
	var parsed sdk.ParsedMedia

	base := filepath.Base(path)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	dir := filepath.Dir(path)
	leafFolder := filepath.Base(dir)

	tags := probe.Tags // lowercased keys (scan lowercases them)

	// 1. Title — tags-primary. The "album" tag is the book; "title" on a
	// per-chapter file is the chapter, so album wins for the book title.
	parsed.Title = strings.TrimSpace(tags["album"])
	if parsed.Title == "" {
		// No album tag: the leaf folder usually names the book (the
		// "Author/Series ##/Title/" convention). Two exceptions fall back to the
		// file stem (stripped of any leading part ordinal): a degenerate leaf
		// (cleanName == "" for a root-relative path), and a flat layout where the
		// file sits directly in the AUTHOR folder — proven when an author tag
		// (album_artist/artist) matches the leaf folder, so the folder is the
		// author and the title lives in the filename.
		folder := cleanName(leafFolder)
		if folder == "" || folderIsAuthor(folder, tags) {
			parsed.Title = firstNonEmpty(cleanName(stripLeadingOrdinal(stem)), folder)
		} else {
			parsed.Title = folder
		}
	}

	// 2. Part ordinal within the book (Index). The container "track" tag wins;
	// otherwise a leading number on the filename ("01 - Chapter One.mp3"). A
	// non-positive numerator (zero, negative, or missing) is not a valid part
	// ordinal, so it is treated as absent rather than clamped — this permits
	// the filename fallback below instead of parking the file in a bogus
	// Parts group at ordinal 0 or less.
	if track, total := trackNumber(tags["track"]); track != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(track)); err == nil && n > 0 {
			parsed.Index = n
			parsed.IndexEnd = atoi(total)
			parsed.PartGroup = filepath.Clean(dir)
		}
	}
	if parsed.PartGroup == "" {
		if m := reLeadingOrdinal.FindStringSubmatch(stem); m != nil {
			parsed.Index = atoi(m[1])
			if parsed.Index > 0 {
				parsed.PartGroup = filepath.Clean(dir)
			}
		}
	}

	// 3. Series position (Sequence) from the folder convention
	// "Author/Series ##/Title/" — the grandparent segment carries the series
	// name + number. The book title folder is the leaf; its parent is the
	// "Series ##" segment.
	seriesSegment := filepath.Base(filepath.Dir(dir))
	if m := reSeriesFolder.FindStringSubmatch(cleanSeriesSegment(seriesSegment)); m != nil {
		parsed.Sequence = m[2]
	}

	if parsed.Title == "" {
		return sdk.ParsedMedia{}, false
	}
	parsed.Class = ClassAudiobook
	return parsed, true
}

// ParseWithContext refuses to fold a multi-file directory unless embedded
// book and ordering tags independently prove the grouping. Single-file and
// absent/future context preserve Parse's established path fallback.
func (p *Parser) ParseWithContext(path string, probe sdk.ParseProbe, context sdk.ParseContext) (sdk.ParsedMedia, bool) {
	parsed, ok := p.Parse(path, probe)
	if context.DirectoryMediaFileCount != sdk.DirectoryMediaFileCountMultiple || !ok {
		return parsed, ok
	}
	track, _ := trackNumber(probe.Tags["track"])
	ordinal, err := strconv.Atoi(track)
	if strings.TrimSpace(probe.Tags["album"]) == "" || err != nil || ordinal <= 0 {
		return sdk.ParsedMedia{}, false
	}
	return parsed, true
}

// PartMarkerTitle returns the chapter-navigation label for one file in a
// parser-confirmed Parts group. The embedded per-track title is authoritative;
// otherwise the filename stem, without its ordering prefix, is the label. Core
// discovers this optional parser method through its consumer-owned interface,
// keeping audiobook vocabulary and filename heuristics inside the plugin.
func (p *Parser) PartMarkerTitle(path string, probe sdk.ParseProbe) string {
	if title := strings.TrimSpace(probe.Tags["title"]); title != "" {
		return title
	}
	base := filepath.Base(path)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	return cleanName(stripLeadingOrdinal(stem))
}

// ParseMediaFields derives flat Media fields from embedded tags. Tags are the
// narrator source because the official/open scrapers do not carry narrator
// reliably.
func (p *Parser) ParseMediaFields(_ string, probe sdk.ParseProbe) []sdk.ParsedTextField {
	narrator := firstNonEmpty(probe.Tags["narrator"], probe.Tags["composer"])
	if narrator == "" {
		return nil
	}
	return []sdk.ParsedTextField{{Name: "narrator", Text: narrator}}
}

// trackNumber extracts the leading number and optional total from an ID3 track
// tag, which may be "5" or "5/12" (track-of-total). Returns "" when empty.
func trackNumber(raw string) (string, string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}
	if track, total, ok := strings.Cut(raw, "/"); ok {
		return strings.TrimSpace(track), strings.TrimSpace(total)
	}
	return raw, ""
}

// stripLeadingOrdinal removes a leading "NN - " / "Track NN " prefix so a
// per-file fallback title is the chapter name, not the number. The regex is
// case-insensitive and matched against name itself, so loc is a valid byte
// offset into name (matching a lowercased copy would garble the slice for
// runes whose lowercase form has a different byte length).
func stripLeadingOrdinal(name string) string {
	loc := reLeadingOrdinal.FindStringIndex(name)
	if loc == nil || loc[0] != 0 {
		return name
	}
	return strings.TrimLeft(name[loc[1]:], " ._-")
}

// cleanName turns a raw path segment into a display string: `.`/`_` → spaces,
// whitespace collapsed. Degenerate path bases ("/", ".") that filepath.Base
// can yield for rootless or extension-only paths clean to "" — not a title.
func cleanName(raw string) string {
	if raw == "." || raw == string(filepath.Separator) {
		return ""
	}
	replaced := cleanNameReplacer.Replace(raw)
	return strings.Join(strings.Fields(replaced), " ")
}

func cleanSeriesSegment(raw string) string {
	if raw == "." || raw == string(filepath.Separator) {
		return ""
	}
	return strings.Join(strings.Fields(strings.ReplaceAll(raw, "_", " ")), " ")
}

// folderIsAuthor reports whether a leaf folder being used as the title fallback
// is really the author folder (a flat layout with no Title folder), evidenced by
// an author tag — album_artist, else artist — that matches it. In that layout
// the book title lives in the filename, not the folder.
func folderIsAuthor(folder string, tags map[string]string) bool {
	author := cleanName(firstNonEmpty(tags["album_artist"], tags["artist"]))
	return author != "" && strings.EqualFold(author, folder)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v != "" {
			return v
		}
	}
	return ""
}

func atoi(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}
