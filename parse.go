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

	// A disc subfolder of a multi-disc rip: "Disc 1", "disc01", "Disk 3",
	// "CD2", "CD 02". Captures the disc number; bookDir accepts 1–999. Only
	// these keywords qualify: "Part 1" or "Volume 2" folders are not discs.
	reDiscFolder = regexp.MustCompile(`(?i)^(?:disc|disk|cd)[ ._-]?(\d{1,3})$`)

	cleanNameReplacer = strings.NewReplacer(".", " ", "_", " ")
)

// discStride separates disc-qualified part ordinals: a part in disc d with
// track t has Index d*discStride + t. It exceeds every filename ordinal
// (reLeadingOrdinal reads at most four digits), so disc order always wins
// over track order; a track tag at or above it is not a valid ordinal in a
// disc folder.
const discStride = 10000

// Parse derives a best-guess ParsedMedia for one audiobook file. Tags win when
// present (tags-primary); the folder/filename convention is the
// fallback. ok is true only when a non-empty Title was derived.
//
// Field mapping onto the domain-agnostic seam (sdk.ParsedMedia, named by
// shape):
//   - Title    — the book title (tag "album", else the leaf folder name; a
//     disc subfolder's parent folder name, whatever the tags).
//   - Class    — always "audiobook" when a title is found (single MediaClass).
//   - Sequence — the book's position in its Series (folder "Series ## /");
//     empty when the book is not part of a recognised series.
//   - Index    — this file's part ordinal within the book (track tag, else a
//     leading filename number), 0 when single-file or unknown. In a disc
//     subfolder ("Title/Disc 2/") it is disc*discStride + that ordinal.
//   - PartGroup — the leaf folder key for ordered Parts when Index is known;
//     for a disc subfolder, its parent (the book folder).
func (p *Parser) Parse(path string, probe sdk.ParseProbe) (sdk.ParsedMedia, bool) {
	var parsed sdk.ParsedMedia

	base := filepath.Base(path)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	dir, disc := bookDir(filepath.Dir(path))
	leafFolder := filepath.Base(dir)

	tags := probe.Tags // lowercased keys (scan lowercases them)

	// 1. Title — tags-primary. The "album" tag is the book; "title" on a
	// per-chapter file is the chapter, so album wins for the book title.
	// Disc subfolders are the exception: the book folder titles them whatever
	// the tags say, because rips often tag each disc's album differently
	// ("Novel (Disc 1)"), and a rescan parses an unchanged single-file disc
	// without its tags, so only a path-derived title keeps every disc on one
	// grouping key. bookDir guarantees that folder name is not empty.
	if disc > 0 {
		parsed.Title = cleanName(leafFolder)
	} else {
		parsed.Title = strings.TrimSpace(tags["album"])
	}
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
		if n, err := strconv.Atoi(strings.TrimSpace(track)); err == nil && validTrack(n, disc) {
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
	// A disc subfolder qualifies the ordinal so every disc's parts join one
	// book in disc-then-track order. The disc number alone orders a disc that
	// holds a single file. A per-disc track total does not bound the
	// qualified Index, so it is dropped.
	if disc > 0 {
		parsed.Index = disc*discStride + parsed.Index
		parsed.IndexEnd = 0
		parsed.PartGroup = filepath.Clean(dir)
	}

	// 3. Series position (Sequence) from the folder convention
	// "Author/Series ##/Title/" — the grandparent segment carries the series
	// name + number. The book title folder is the leaf (or the parent of a
	// disc subfolder); its parent is the "Series ##" segment.
	_, parsed.Sequence = seriesFromFolder(path)

	if parsed.Title == "" {
		return sdk.ParsedMedia{}, false
	}
	parsed.Class = ClassAudiobook
	return parsed, true
}

// ParseWithContext decides whether a file in a multi-file directory may fold
// into that directory's ordered Parts. Single-file and absent/future context
// preserve Parse's established path fallback. In a multi-file directory a file
// is accepted when either:
//   - its embedded tags prove the grouping: a book ("album") tag and a positive
//     part ordinal ("track") tag (CORE-1710 behaviour, unchanged); or
//   - it carries neither tag and its filename starts with a positive part
//     number ("01.mp3", "Part 2.mp3", "Chapter 10 - Name.mp3"), so the folder
//     names the book and the number orders the part (CORE-1834).
//
// Core orders a Parts group by the numeric Index, so "Part 2" sorts before
// "Part 10" without any lexical comparison. Every other file is refused;
// ContextRefusalReason says why. Core refuses a whole folder when any of its
// audio files is refused, so a folder that mixes numbered parts with unrelated
// files stays out of the library.
func (p *Parser) ParseWithContext(path string, probe sdk.ParseProbe, context sdk.ParseContext) (sdk.ParsedMedia, bool) {
	parsed, ok := p.Parse(path, probe)
	if context.DirectoryMediaFileCount == sdk.DirectoryMediaFileCountSingle && ok {
		// A rescan parses an unchanged single-file folder without its tags,
		// so a lone file in a disc folder is ordered by the disc alone.
		if _, disc := bookDir(filepath.Dir(path)); disc > 0 {
			parsed.Index = disc * discStride
		}
	}
	if context.DirectoryMediaFileCount != sdk.DirectoryMediaFileCountMultiple || !ok {
		return parsed, ok
	}
	if multiFileRefusal(path, probe, parsed) != "" {
		return sdk.ParsedMedia{}, false
	}
	return parsed, true
}

// Refusal reasons for a file in a multi-file directory. They name the evidence
// that is missing, so a refused folder can be fixed by tagging or renaming.
const (
	RefusalTaggedWithoutBook    = "file has a part ordinal tag but no book (album) tag"
	RefusalTaggedWithoutOrdinal = "file has a book (album) tag but no positive part ordinal (track) tag"
	RefusalUntaggedNoPartNumber = "untagged file does not start with a positive part number"
	RefusalFolderNotBookTitle   = "untagged file's folder does not name the book"
)

// ContextRefusalReason returns why ParseWithContext refuses path, or "" when
// it accepts it or when Parse itself finds no title. Core can discover it
// through a consumer-owned interface, as it does PartMarkerTitle, to show the
// reason beside a refused folder.
func (p *Parser) ContextRefusalReason(path string, probe sdk.ParseProbe, context sdk.ParseContext) string {
	parsed, ok := p.Parse(path, probe)
	if context.DirectoryMediaFileCount != sdk.DirectoryMediaFileCountMultiple || !ok {
		return ""
	}
	return multiFileRefusal(path, probe, parsed)
}

func multiFileRefusal(path string, probe sdk.ParseProbe, parsed sdk.ParsedMedia) string {
	album := strings.TrimSpace(probe.Tags["album"])
	rawTrack := strings.TrimSpace(probe.Tags["track"])
	track, _ := trackNumber(rawTrack)
	if album != "" || rawTrack != "" {
		ordinal, err := strconv.Atoi(track)
		_, disc := bookDir(filepath.Dir(path))
		switch {
		case album == "":
			return RefusalTaggedWithoutBook
		case err != nil || !validTrack(ordinal, disc):
			return RefusalTaggedWithoutOrdinal
		}
		return ""
	}
	// A disc folder's number alone orders a single-file disc; beside other
	// files each part still needs its own number.
	if parsed.PartGroup == "" || parsed.Index%discStride <= 0 {
		return RefusalUntaggedNoPartNumber
	}
	// The folder is the book only when Parse titled the file from it and an
	// author tag does not name the folder: in the flat author-folder layout
	// the folder is the author, even when a bare-number name leaves Parse no
	// other title. A disc subfolder's parent is the book folder.
	book, _ := bookDir(filepath.Dir(path))
	folder := cleanName(filepath.Base(book))
	if folder == "" || parsed.Title != folder || folderIsAuthor(folder, probe.Tags) {
		return RefusalFolderNotBookTitle
	}
	return ""
}

// PartMarkerTitle returns the chapter-navigation label for one file in a
// parser-confirmed Parts group. The embedded per-track title is authoritative;
// otherwise the filename stem, without its ordering prefix, is the label. In a
// disc subfolder the filename label is prefixed with the disc ("Disc 2 · The
// Door"), and a bare-number file is labelled by disc and part ("Disc 2 · Part
// 3"), because part numbers restart on each disc. Core discovers this optional
// parser method through its consumer-owned interface, keeping audiobook
// vocabulary and filename heuristics inside the plugin.
func (p *Parser) PartMarkerTitle(path string, probe sdk.ParseProbe) string {
	if title := strings.TrimSpace(probe.Tags["title"]); title != "" {
		return title
	}
	base := filepath.Base(path)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	label := cleanName(stripLeadingOrdinal(stem))
	_, disc := bookDir(filepath.Dir(path))
	if disc == 0 {
		return label
	}
	prefix := "Disc " + strconv.Itoa(disc)
	if label == "" {
		parsed, _ := p.Parse(path, probe)
		if part := parsed.Index % discStride; part > 0 {
			label = "Part " + strconv.Itoa(part)
		}
	}
	if label == "" {
		return prefix
	}
	return prefix + " · " + label
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

// bookDir returns the folder that names the book for a file in dir, and the
// disc number when dir is a disc subfolder ("Author/Title/Disc 2/") whose
// parent is a real folder: the parent is then the book folder. Otherwise it
// returns dir and 0. A disc folder directly under the library root cannot be
// told apart from one under a book folder, so it takes the root's name.
func bookDir(dir string) (string, int) {
	m := reDiscFolder.FindStringSubmatch(filepath.Base(dir))
	if m == nil {
		return dir, 0
	}
	disc := atoi(m[1])
	parent := filepath.Dir(dir)
	if disc <= 0 || cleanName(filepath.Base(parent)) == "" {
		return dir, 0
	}
	return parent, disc
}

// validTrack reports whether n is a usable part ordinal from a track tag: it
// must be positive, and in a disc subfolder below discStride so the
// disc-qualified Index stays unambiguous.
func validTrack(n, disc int) bool {
	return n > 0 && (disc == 0 || n < discStride)
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

// seriesFromFolder reads the "Author/Series ##/Title/file" convention: the
// segment above the book folder names the series and carries its position.
// That is path's grandparent, or one level higher for a disc subfolder
// ("Author/Series ##/Title/Disc 1/file").
// Both results are "" when the segment does not match; name alone is "" for a
// bare-number segment ("03").
func seriesFromFolder(path string) (name, sequence string) {
	book, _ := bookDir(filepath.Dir(path))
	segment := filepath.Base(filepath.Dir(book))
	m := reSeriesFolder.FindStringSubmatch(cleanSeriesSegment(segment))
	if m == nil {
		return "", ""
	}
	return strings.TrimSpace(m[1]), m[2]
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
