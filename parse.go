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
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

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
	reLeadingOrdinal      = regexp.MustCompile(`(?i)^(?:track|chapter|part|disc|cd|ch|pt)?[ ._-]*(\d{1,4})(?:\b|_)`)
	reFilenameDiscTrack   = regexp.MustCompile(`(?i)^(?:disc|disk|cd)[ ._-]*(\d{1,3})[ ._/-]+(?:track|chapter|part|ch|pt)?[ ._-]*(\d{1,4})(?:\b|_)`)
	reTrailingPartOrdinal = regexp.MustCompile(`(?i)(?:^|[ ._(-]+)(?:part|chapter|track|pt|ch)[ ._-]*(\d{1,4})[\)\s]*$`)
	reKeywordOrdinalOnly  = regexp.MustCompile(`(?i)^(track|chapter|part|disc|disk|cd|ch|pt)[ ._-]*0*(\d{1,4})$`)
	reDigitsOnly          = regexp.MustCompile(`^0*(\d{1,4})$`)
	reNoiseSuffix         = regexp.MustCompile(`(?i)(?:\s*[\(\[\{][^)\]\}]*\b(?:unabridged|abridged|audiobook|mp3|m4b|m4a|aac|flac|ogg|opus|wma|cbr|vbr|retail|(?:\d+\s*)?kbps|read\s+by\b[^)\]\}]*|narrated\s+by\b[^)\]\}]*)[\)\]\}]|\s*[-–—]\s*(?:unabridged|abridged|audiobook|retail)\b)\s*$`)
	reBracketedDisc       = regexp.MustCompile(`(?i)(?:\s*[,\-–—]\s*|\s*)[\(\[\{]\s*(?:disc|disk|cd|tape)[ ._-]*0*([1-9]\d{0,2})\s*[\)\]\}]`)
	reTrailingDisc        = regexp.MustCompile(`(?i)(?:^|\s*[,\-–—]\s*|\s+)(?:disc|disk|cd|tape)[ ._-]*0*([1-9]\d{0,2})\s*$`)
	reInlineDisc          = regexp.MustCompile(`(?i)(?:^|[ ._/-]+)(?:disc|disk|cd|tape)[ ._-]*0*([1-9]\d{0,2})(?:$|[ ._/-]+)`)
	reLeadingDiscOnly     = regexp.MustCompile(`(?i)^(?:disc|disk|cd|tape)[ ._-]*0*[1-9]\d{0,2}(?:\b|_)`)
	reTrailingStemPart    = regexp.MustCompile(`(?i)(?:(?:\s*[-–—]\s*|\s+)(?:part|chapter|disc|disk|cd|pt|ch)[ ._-]*\d{1,4}|\s*[-–—]\s*\d{1,3})\s*$`)
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
	rawAlbum := strings.TrimSpace(tagValue(tags, "album"))
	effDisc := effectiveDisc(disc, stem, tags)
	_, fnDiscOrd, hasFnDisc := parseFilenameDiscTrack(stem)
	stemClean, _ := stripAndExtractDisc(stem)
	author := tagValue(tags, "album_artist", "artist")

	_, parsed.Sequence = seriesFromFolder(path)

	// 1. Title — tags-primary. The "album" tag is the book; "title" on a
	// per-chapter file is the chapter, so album wins for the book title.
	// Disc subfolders are the exception: the book folder titles them strictly from
	// path layout (ignoring embedded author/album tags), because rips often tag each
	// disc's album differently ("Novel (Disc 1)"), and an untagged fast-path rescan
	// of a single-file disc must still agree with multi-file disc folders.
	if disc > 0 {
		parsed.Title = cleanBookFolderTitle(leafFolder, "", bookParentFolder(dir, parsed.Sequence))
	} else if rawAlbum != "" {
		parsed.Title = cleanAlbumTitle(rawAlbum, author, bookParentFolder(dir, parsed.Sequence))
	}
	if parsed.Title == "" {
		// No album tag: the leaf folder usually names the book (the
		// "Author/Series ##/Title/" convention). Two exceptions fall back to the
		// file stem (stripped of any leading part ordinal): a degenerate leaf
		// (cleanName == "" for a root-relative path), and a flat layout where the
		// file sits directly in the AUTHOR folder — proven when an author tag
		// (album_artist/artist) matches the leaf folder, so the folder is the
		// author and the title lives in the filename.
		folder := cleanBookFolderTitle(leafFolder, author, bookParentFolder(dir, parsed.Sequence))
		if folder == "" || folderIsAuthor(folder, tags) {
			stemTitle := strings.TrimSpace(reTrailingStemPart.ReplaceAllString(stemClean, ""))
			if stemTitle == "" {
				stemTitle = stemClean
			}
			parsed.Title = firstNonEmpty(cleanName(stripLeadingOrdinal(stemTitle)), folder)
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
	if track, total := trackTag(tags); track != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(track)); err == nil && validTrack(n, effDisc) {
			parsed.Index = n
			parsed.IndexEnd = atoi(total)
			parsed.PartGroup = filepath.Clean(dir)
		}
	}
	if parsed.PartGroup == "" && hasFnDisc && validTrack(fnDiscOrd, effDisc) {
		parsed.Index = fnDiscOrd
		parsed.PartGroup = filepath.Clean(dir)
	}
	if parsed.PartGroup == "" && !reLeadingDiscOnly.MatchString(stem) {
		if m := reLeadingOrdinal.FindStringSubmatch(stem); m != nil {
			if n := atoi(m[1]); validTrack(n, effDisc) {
				parsed.Index = n
				parsed.PartGroup = filepath.Clean(dir)
			}
		}
	}
	if parsed.PartGroup == "" {
		if m := reTrailingPartOrdinal.FindStringSubmatch(stem); m != nil {
			if n := atoi(m[1]); validTrack(n, effDisc) {
				parsed.Index = n
				parsed.PartGroup = filepath.Clean(dir)
			}
		}
	}
	// A disc subfolder, disc tag, or disc marker in album/filename qualifies the
	// ordinal so every disc's parts join one book in disc-then-track order. The
	// disc number alone orders a disc folder or single-file disc. A per-disc
	// track total does not bound the qualified Index, so it is dropped.
	if effDisc > 0 && parsed.PartGroup != "" {
		parsed.Index = effDisc*discStride + parsed.Index
		parsed.IndexEnd = 0
	} else if effDisc > 0 {
		parsed.Index = effDisc * discStride
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

// ParseWithContext validates per-file grouping inside multi-file directories when
// invoked without full-directory context. Full-directory ingestion uses
// ParseDirectory so partial tags and author-prefixed/unnumbered siblings stay
// grouped as one book.
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
// that is missing or conflicting, so a refused folder can be fixed cleanly.
const (
	RefusalTaggedWithoutBook    = "file has a part ordinal tag but no book (album) tag"
	RefusalTaggedWithoutOrdinal = "file has a book (album) tag but no positive part ordinal (track) tag"
	RefusalUntaggedNoPartNumber = "untagged file does not start with a positive part number"
	RefusalFolderNotBookTitle   = "untagged file's folder does not name the book"
	RefusalConflictingAlbumTags = "folder mixes files with conflicting book (album) tags"
)

// ContextRefusalReason returns why ParseWithContext refuses path, or "" when
// it accepts it or when Parse itself finds no title. Core discovers it through
// a consumer-owned interface to log and store the refusal reason.
func (p *Parser) ContextRefusalReason(path string, probe sdk.ParseProbe, context sdk.ParseContext) string {
	parsed, ok := p.Parse(path, probe)
	if context.DirectoryMediaFileCount != sdk.DirectoryMediaFileCountMultiple || !ok {
		return ""
	}
	return multiFileRefusal(path, probe, parsed)
}

func multiFileRefusal(path string, probe sdk.ParseProbe, parsed sdk.ParsedMedia) string {
	album := strings.TrimSpace(tagValue(probe.Tags, "album"))
	rawTrack := strings.TrimSpace(tagValue(probe.Tags, "track", "tracknumber", "trck"))
	if album != "" || rawTrack != "" {
		track, _ := trackNumber(rawTrack)
		ordinal, err := strconv.Atoi(track)
		_, disc := bookDir(filepath.Dir(path))
		base := filepath.Base(path)
		stem := strings.TrimSuffix(base, filepath.Ext(base))
		effDisc := effectiveDisc(disc, stem, probe.Tags)
		if rawTrack != "" && (err != nil || !validTrack(ordinal, effDisc)) {
			if album == "" {
				return RefusalTaggedWithoutBook
			}
			return RefusalTaggedWithoutOrdinal
		}
		if album != "" && (parsed.PartGroup == "" || parsed.Index%discStride <= 0) {
			return RefusalTaggedWithoutOrdinal
		}
		if parsed.Title == "" {
			return RefusalFolderNotBookTitle
		}
		return ""
	}
	// A disc folder's number alone orders a single-file disc; beside other
	// files each part still needs its own number when parsed in isolation.
	if parsed.PartGroup == "" || parsed.Index%discStride <= 0 {
		return RefusalUntaggedNoPartNumber
	}
	book, _ := bookDir(filepath.Dir(path))
	author := tagValue(probe.Tags, "album_artist", "artist")
	parent := filepath.Base(filepath.Dir(book))
	folder := cleanBookFolderTitle(filepath.Base(book), author, parent)
	if folder == "" {
		return RefusalFolderNotBookTitle
	}
	return ""
}

// ParseDirectory evaluates all non-image media files in a flat directory together.
// It groups a folder of audio files into one ordered book even with partial tags,
// author-prefixed or noise-suffixed folders, or mixed numbered/unnumbered names,
// refusing only when files declare two or more distinct conflicting album tags.
// When hasMediaSubdirs is true (disc subdirs alongside loose files) or fewer
// than two files exist, handled=false delegates back to per-file context parsing.
func (p *Parser) ParseDirectory(dir string, probes map[string]sdk.ParseProbe, hasMediaSubdirs bool) (map[string]sdk.ParsedMedia, string, bool) {
	if hasMediaSubdirs || len(probes) < 2 {
		return nil, "", false
	}
	paths := make([]string, 0, len(probes))
	for path := range probes {
		paths = append(paths, path)
	}
	slices.Sort(paths)

	book, discDir := bookDir(dir)
	leafFolder := filepath.Base(book)
	_, seq := seriesFromFolder(filepath.Join(dir, "_"))
	parentFolder := bookParentFolder(book, seq)

	dirAuthor := ""
	for _, path := range paths {
		if a := tagValue(probes[path].Tags, "album_artist", "artist"); a != "" {
			dirAuthor = a
			break
		}
	}

	albumTitles := map[string]string{}
	firstAlbumKey := ""
	for _, path := range paths {
		rawAlbum := strings.TrimSpace(tagValue(probes[path].Tags, "album"))
		if rawAlbum == "" {
			continue
		}
		cleaned := cleanAlbumTitle(rawAlbum, dirAuthor, parentFolder)
		if cleaned == "" {
			continue
		}
		key := albumComparisonKey(rawAlbum, dirAuthor, parentFolder)
		if key == "" {
			key = strings.ToLower(cleaned)
		}
		if _, exists := albumTitles[key]; !exists {
			if firstAlbumKey == "" {
				firstAlbumKey = key
			}
			albumTitles[key] = cleaned
		}
	}
	if len(albumTitles) > 1 {
		return nil, RefusalConflictingAlbumTags, true
	}

	var title string
	if discDir > 0 {
		title = cleanBookFolderTitle(leafFolder, "", parentFolder)
	} else if firstAlbumKey != "" {
		title = albumTitles[firstAlbumKey]
	} else {
		folder := cleanBookFolderTitle(leafFolder, dirAuthor, parentFolder)
		if folder == "" || folderIsAuthor(folder, map[string]string{"artist": dirAuthor}) {
			title = firstNonEmpty(commonStemBookTitle(paths, dirAuthor), folder)
		} else {
			title = folder
		}
	}
	if title == "" {
		return nil, "", false
	}

	type entry struct {
		path     string
		base     string
		stem     string
		effDisc  int
		trackHas bool
		trackN   int
		fnHas    bool
		fnOrd    int
		discRank int
		ordHas   bool
		ord      int
	}
	entries := make([]entry, 0, len(paths))
	distinctDiscs := map[int]struct{}{}
	for _, path := range paths {
		base := filepath.Base(path)
		stem := strings.TrimSuffix(base, filepath.Ext(base))
		tags := probes[path].Tags
		dt := discTag(tags)
		_, albumDisc := cleanAlbumAndDisc(tagValue(tags, "album"), "", "")
		fnDisc, fnDiscOrd, hasFnDisc := parseFilenameDiscTrack(stem)
		_, stemDisc := stripAndExtractDisc(stem)
		effDisc := dt
		if effDisc == 0 {
			effDisc = albumDisc
		}
		if effDisc == 0 && discDir == 0 && hasFnDisc {
			effDisc = fnDisc
		}
		if effDisc == 0 && discDir == 0 {
			effDisc = stemDisc
		}
		if effDisc > 0 && discDir == 0 {
			distinctDiscs[effDisc] = struct{}{}
		}

		trackN, trackHas := 0, false
		if tr, _ := trackTag(tags); tr != "" {
			if n, err := strconv.Atoi(tr); err == nil && n >= 0 {
				trackN, trackHas = n, true
			}
		}

		fnOrd, fnHas := 0, false
		switch {
		case hasFnDisc:
			fnOrd, fnHas = fnDiscOrd, true
		case reLeadingOrdinal.MatchString(stem):
			m := reLeadingOrdinal.FindStringSubmatch(stem)
			fnOrd, fnHas = atoi(m[1]), true
		case reTrailingPartOrdinal.MatchString(stem):
			m := reTrailingPartOrdinal.FindStringSubmatch(stem)
			fnOrd, fnHas = atoi(m[1]), true
		}

		entries = append(entries, entry{
			path:     path,
			base:     base,
			stem:     stem,
			effDisc:  effDisc,
			trackHas: trackHas,
			trackN:   trackN,
			fnHas:    fnHas,
			fnOrd:    fnOrd,
		})
	}

	singleDisc := 0
	if len(distinctDiscs) == 1 {
		for d := range distinctDiscs {
			singleDisc = d
		}
	}
	hasAnyDisc := len(distinctDiscs) > 0
	for i := range entries {
		e := &entries[i]
		switch {
		case discDir > 0:
			e.discRank = discDir
		case e.effDisc > 0:
			e.discRank = e.effDisc
		case !hasAnyDisc:
			e.discRank = 0
		case singleDisc > 0:
			e.discRank = singleDisc
		default:
			e.discRank = 1 << 30
		}
		if e.trackHas {
			e.ordHas, e.ord = true, e.trackN
		} else if e.fnHas {
			e.ordHas, e.ord = true, e.fnOrd
		}
	}

	slices.SortFunc(entries, func(a, b entry) int {
		if a.discRank != b.discRank {
			return cmpInt(a.discRank, b.discRank)
		}
		if a.ordHas != b.ordHas {
			if a.ordHas {
				return -1
			}
			return 1
		}
		if a.ord != b.ord {
			return cmpInt(a.ord, b.ord)
		}
		if a.fnHas != b.fnHas {
			if a.fnHas {
				return -1
			}
			return 1
		}
		if a.fnOrd != b.fnOrd {
			return cmpInt(a.fnOrd, b.fnOrd)
		}
		if c := naturalCompare(a.stem, b.stem); c != 0 {
			return c
		}
		if a.base != b.base {
			return strings.Compare(a.base, b.base)
		}
		return strings.Compare(a.path, b.path)
	})

	groupKey := filepath.Clean(book)
	out := make(map[string]sdk.ParsedMedia, len(entries))
	for i, e := range entries {
		idx := i + 1
		if discDir > 0 {
			idx = discDir*discStride + i + 1
		}
		out[e.path] = sdk.ParsedMedia{
			Class:     ClassAudiobook,
			Title:     title,
			Sequence:  seq,
			Index:     idx,
			PartGroup: groupKey,
		}
	}
	return out, "", true
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
	if title := strings.TrimSpace(tagValue(probe.Tags, "title")); title != "" {
		return title
	}
	base := filepath.Base(path)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	label := cleanName(stripLeadingOrdinal(stem))
	_, disc := bookDir(filepath.Dir(path))
	if disc == 0 {
		if label != "" {
			return label
		}
		return cleanOrdinalStemLabel(stem)
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
	narrator := firstNonEmpty(tagValue(probe.Tags, "narrator", "composer"))
	if narrator == "" {
		return nil
	}
	return []sdk.ParsedTextField{{Name: "narrator", Text: narrator}}
}

var wrapperFolders = map[string]bool{
	"audiobook": true,
	"audio":     true,
	"mp3":       true,
	"m4b":       true,
}

// bookDir returns the folder that names the book for a file in dir, and the
// disc number when dir is a disc subfolder ("Author/Title/Disc 2/") whose
// parent is a real folder: the parent is then the book folder. Wrapper
// subfolders ("Audiobook", "Audio", "MP3", "M4B", or a child folder whose
// cleaned title matches the parent folder's cleaned title) are also flattened
// onto the parent book folder.
func bookDir(dir string) (string, int) {
	disc := 0
	peelDisc := func(d string) (string, int) {
		if m := reDiscFolder.FindStringSubmatch(filepath.Base(d)); m != nil {
			n := atoi(m[1])
			p := filepath.Dir(d)
			if n > 0 && cleanName(filepath.Base(p)) != "" {
				return p, n
			}
		}
		return d, 0
	}
	if next, d := peelDisc(dir); d > 0 {
		dir, disc = next, d
	}
	base := filepath.Base(dir)
	parent := filepath.Dir(dir)
	parentBase := filepath.Base(parent)
	if cleanName(parentBase) != "" {
		flattens := wrapperFolders[strings.ToLower(strings.TrimSpace(base))]
		if !flattens {
			childClean := cleanBookFolderTitle(base, "", "")
			grandparent := filepath.Base(filepath.Dir(parent))
			parentClean := cleanBookFolderTitle(parentBase, "", grandparent)
			flattens = childClean != "" && parentClean != "" && strings.EqualFold(childClean, parentClean)
		}
		if flattens {
			dir = parent
			if disc == 0 {
				if next, d := peelDisc(dir); d > 0 {
					dir, disc = next, d
				}
			}
		}
	}
	return dir, disc
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
	author := cleanName(tagValue(tags, "album_artist", "artist"))
	return author != "" && strings.EqualFold(author, folder)
}

func tagValue(tags map[string]string, keys ...string) string {
	for _, key := range keys {
		if v := strings.TrimSpace(tags[key]); v != "" {
			return v
		}
	}
	for _, want := range keys {
		bestKey := ""
		bestVal := ""
		for k, v := range tags {
			if strings.EqualFold(k, want) {
				if trimmed := strings.TrimSpace(v); trimmed != "" {
					if bestKey == "" || k < bestKey {
						bestKey = k
						bestVal = trimmed
					}
				}
			}
		}
		if bestVal != "" {
			return bestVal
		}
	}
	return ""
}

func bookParentFolder(book, seq string) string {
	parent := filepath.Base(filepath.Dir(book))
	if seq != "" {
		if gp := filepath.Base(filepath.Dir(filepath.Dir(book))); gp != "" && gp != "." && gp != string(filepath.Separator) {
			return gp
		}
	}
	return parent
}

func trackTag(tags map[string]string) (string, string) {
	return trackNumber(tagValue(tags, "track", "tracknumber", "trck"))
}

func discTag(tags map[string]string) int {
	raw, _ := trackNumber(tagValue(tags, "disc", "discnumber", "discnum", "tpos"))
	n := atoi(raw)
	if n < 1 || n > 999 {
		return 0
	}
	return n
}

func effectiveDisc(dirDisc int, stem string, tags map[string]string) int {
	if dirDisc > 0 {
		return dirDisc
	}
	if dt := discTag(tags); dt > 0 {
		return dt
	}
	if _, albumDisc := cleanAlbumAndDisc(tagValue(tags, "album"), "", ""); albumDisc > 0 {
		return albumDisc
	}
	if fnDisc, _, ok := parseFilenameDiscTrack(stem); ok && fnDisc > 0 {
		return fnDisc
	}
	if _, stemDisc := stripAndExtractDisc(stem); stemDisc > 0 {
		return stemDisc
	}
	return 0
}

func stripNoiseSuffixes(s string) string {
	for {
		loc := reNoiseSuffix.FindStringIndex(s)
		if loc == nil {
			return strings.TrimSpace(s)
		}
		trimmed := strings.TrimSpace(s[:loc[0]])
		if trimmed == "" {
			return strings.TrimSpace(s)
		}
		s = trimmed
	}
}

func stripAndExtractDisc(s string) (string, int) {
	disc := 0
	cur := strings.TrimSpace(s)
	for {
		changed := false
		if loc := reBracketedDisc.FindStringSubmatchIndex(cur); loc != nil {
			if disc == 0 {
				disc = atoi(cur[loc[2]:loc[3]])
			}
			cur = strings.Join(strings.Fields(cur[:loc[0]]+" "+cur[loc[1]:]), " ")
			cur = strings.TrimRight(cur, " ,-–—")
			cur = strings.TrimSpace(cur)
			changed = true
		} else if loc := reTrailingDisc.FindStringSubmatchIndex(cur); loc != nil {
			if disc == 0 {
				disc = atoi(cur[loc[2]:loc[3]])
			}
			cur = strings.TrimSpace(cur[:loc[0]])
			changed = true
		} else if loc := reInlineDisc.FindStringSubmatchIndex(cur); loc != nil {
			if disc == 0 {
				disc = atoi(cur[loc[2]:loc[3]])
			}
			cur = strings.Join(strings.Fields(cur[:loc[0]]+" "+cur[loc[1]:]), " ")
			cur = strings.Trim(cur, " ,-–—")
			changed = true
		}
		if !changed {
			return cur, disc
		}
	}
}

func stripAuthorPrefix(s, author, parent string) string {
	for _, sep := range []string{" - ", " – ", " — "} {
		idx := strings.Index(s, sep)
		if idx <= 0 {
			continue
		}
		prefix := strings.TrimSpace(s[:idx])
		rest := strings.TrimSpace(s[idx+len(sep):])
		if rest == "" {
			continue
		}
		if matchesAuthorCandidate(prefix, author) || matchesAuthorCandidate(prefix, cleanName(parent)) {
			return rest
		}
	}
	return s
}

func matchesAuthorCandidate(prefix, candidate string) bool {
	p := cleanName(prefix)
	c := cleanName(candidate)
	if p == "" || c == "" {
		return false
	}
	if strings.EqualFold(p, c) {
		return true
	}
	if last, first, ok := strings.Cut(c, ","); ok {
		flipped := strings.TrimSpace(first) + " " + strings.TrimSpace(last)
		if strings.EqualFold(p, flipped) {
			return true
		}
	}
	return false
}

func cleanBookFolderTitle(raw, author, parent string) string {
	s := cleanName(raw)
	if s == "" {
		return ""
	}
	s = stripNoiseSuffixes(s)
	s = stripAuthorPrefix(s, author, parent)
	s = stripNoiseSuffixes(s)
	return strings.TrimSpace(s)
}

func cleanAlbumTitle(raw, author, parent string) string {
	cleaned, _ := cleanAlbumAndDisc(raw, author, parent)
	return cleaned
}

func cleanAlbumAndDisc(raw, author, parent string) (string, int) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", 0
	}
	disc := 0
	stripStep := func(in string) string {
		for {
			s1 := stripNoiseSuffixes(in)
			s2, d := stripAndExtractDisc(s1)
			if disc == 0 && d > 0 {
				disc = d
			}
			if s2 == in {
				return s2
			}
			in = s2
		}
	}
	s = stripStep(s)
	s = stripAuthorPrefix(s, author, parent)
	s = stripStep(s)
	return strings.TrimSpace(s), disc
}

func albumComparisonKey(raw, author, parent string) string {
	cleaned := cleanAlbumTitle(raw, author, parent)
	return strings.ToLower(cleanName(cleaned))
}

func parseFilenameDiscTrack(stem string) (disc, track int, ok bool) {
	m := reFilenameDiscTrack.FindStringSubmatch(stem)
	if m == nil {
		return 0, 0, false
	}
	d := atoi(m[1])
	t := atoi(m[2])
	if d < 1 || d > 999 || t < 0 {
		return 0, 0, false
	}
	return d, t, true
}

func commonStemBookTitle(paths []string, author string) string {
	common := ""
	for _, path := range paths {
		base := filepath.Base(path)
		stem := strings.TrimSuffix(base, filepath.Ext(base))
		candidate := stripAuthorPrefix(cleanName(stem), author, "")
		candidate = stripNoiseSuffixes(candidate)
		candidate = cleanName(stripLeadingOrdinal(candidate))
		candidate = strings.TrimSpace(reTrailingStemPart.ReplaceAllString(candidate, ""))
		if candidate == "" {
			continue
		}
		if common == "" {
			common = candidate
		} else if !strings.EqualFold(common, candidate) {
			return ""
		}
	}
	return common
}

func cleanOrdinalStemLabel(stem string) string {
	trimmed := strings.TrimSpace(strings.TrimRight(stem, " ._-"))
	if m := reKeywordOrdinalOnly.FindStringSubmatch(trimmed); m != nil {
		return normalizeOrdinalKeyword(m[1]) + " " + strconv.Itoa(atoi(m[2]))
	}
	if m := reDigitsOnly.FindStringSubmatch(trimmed); m != nil {
		return "Part " + strconv.Itoa(atoi(m[1]))
	}
	return cleanName(stem)
}

func normalizeOrdinalKeyword(kw string) string {
	switch strings.ToLower(kw) {
	case "cd":
		return "CD"
	case "pt", "part":
		return "Part"
	case "ch", "chapter":
		return "Chapter"
	case "track":
		return "Track"
	case "disc", "disk":
		return "Disc"
	default:
		return "Part"
	}
}

func cmpInt(a, b int) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

func isASCIIDigit(r rune) bool {
	return r >= '0' && r <= '9'
}

func naturalCompare(a, b string) int {
	ia, ib := 0, 0
	for ia < len(a) && ib < len(b) {
		ra, wa := utf8.DecodeRuneInString(a[ia:])
		rb, wb := utf8.DecodeRuneInString(b[ib:])
		if isASCIIDigit(ra) && isASCIIDigit(rb) {
			endA := ia + wa
			for endA < len(a) {
				r, w := utf8.DecodeRuneInString(a[endA:])
				if !isASCIIDigit(r) {
					break
				}
				endA += w
			}
			endB := ib + wb
			for endB < len(b) {
				r, w := utf8.DecodeRuneInString(b[endB:])
				if !isASCIIDigit(r) {
					break
				}
				endB += w
			}
			na := strings.TrimLeft(a[ia:endA], "0")
			nb := strings.TrimLeft(b[ib:endB], "0")
			if len(na) != len(nb) {
				return cmpInt(len(na), len(nb))
			}
			if na != nb {
				return strings.Compare(na, nb)
			}
			// Longer zero-padded token after equal numeric value gets deterministic order.
			if endA-ia != endB-ib {
				return cmpInt(endA-ia, endB-ib)
			}
			ia, ib = endA, endB
			continue
		}
		la, lb := unicode.ToLower(ra), unicode.ToLower(rb)
		if la != lb {
			if la < lb {
				return -1
			}
			return 1
		}
		ia += wa
		ib += wb
	}
	return cmpInt(len(a)-ia, len(b)-ib)
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
