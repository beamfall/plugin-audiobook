package audiobook_test

import (
	"reflect"
	"testing"

	sdk "github.com/beamfall/plugin-sdk"
	"github.com/beamfall/plugin-sdk/sdktest"

	audiobook "github.com/beamfall/plugin-audiobook"
)

// TestParseConformance pins the Audiobook filename/tag parser against the SDK
// scan contract. A change that shifts any of
// these guesses reds CI before it ships.
func TestParseConformance(t *testing.T) {
	sdktest.RunConformance(t, audiobook.New(), []sdktest.ParseVector{
		{
			// Tags-primary: album tag supplies the title, track tag supplies the
			// part ordinal. PartGroup is the cleaned directory path.
			Name: "album+track tags: title and part index from tags",
			Path: "/lib/audiobooks/Brandon Sanderson/The Way of Kings/01 - Prologue.m4b",
			Probe: sdk.ParseProbe{
				Tags: map[string]string{
					"album": "The Way of Kings",
					"track": "1",
				},
			},
			Want: sdk.ParsedMedia{
				Title:     "The Way of Kings",
				Class:     audiobook.ClassAudiobook,
				Index:     1,
				PartGroup: "/lib/audiobooks/Brandon Sanderson/The Way of Kings",
			},
			WantOK: true,
		},
		{
			// track tag "5/12" (track-of-total format): trackNumber extracts
			// "5" and preserves "12" as the known total.
			Name: "track tag with total sets index end",
			Path: "/lib/ab/Author/Book Title/05 chapter.mp3",
			Probe: sdk.ParseProbe{
				Tags: map[string]string{
					"album": "Book Title",
					"track": "5/12",
				},
			},
			Want: sdk.ParsedMedia{
				Title:     "Book Title",
				Class:     audiobook.ClassAudiobook,
				Index:     5,
				IndexEnd:  12,
				PartGroup: "/lib/ab/Author/Book Title",
			},
			WantOK: true,
		},
		{
			// No tags: title falls back to the leaf folder name (cleanName).
			// Series segment "Mistborn 2" matches reSeriesFolder → Sequence="2".
			// Filename has no leading ordinal and no track tag → Index=0, no PartGroup.
			Name: "folder layout: leaf-folder title + series sequence from grandparent",
			Path: "/lib/audiobooks/Brandon Sanderson/Mistborn 2/The Well of Ascension/book.m4b",
			Probe: sdk.ParseProbe{
				Tags: map[string]string{},
			},
			Want: sdk.ParsedMedia{
				Title:    "The Well of Ascension",
				Class:    audiobook.ClassAudiobook,
				Sequence: "2",
			},
			WantOK: true,
		},
		{
			// The "book" keyword form of the series segment, lowercase.
			Name: "folder layout: series sequence via lowercase 'book' keyword",
			Path: "/lib/audiobooks/Brandon Sanderson/Mistborn book 2/The Well of Ascension/book.m4b",
			Probe: sdk.ParseProbe{
				Tags: map[string]string{},
			},
			Want: sdk.ParsedMedia{
				Title:    "The Well of Ascension",
				Class:    audiobook.ClassAudiobook,
				Sequence: "2",
			},
			WantOK: true,
		},
		{
			// MEMHID-PLUGAUDIO-BOOKCASE: the "book" keyword matches regardless
			// of case ("Book"/"BOOK"/mixed), while the digit grammar is
			// unchanged — only the keyword is case-folded.
			Name: "folder layout: series sequence via case-insensitive 'BOOK' keyword",
			Path: "/lib/audiobooks/Brandon Sanderson/Mistborn BOOK 2/The Well of Ascension/book.m4b",
			Probe: sdk.ParseProbe{
				Tags: map[string]string{},
			},
			Want: sdk.ParsedMedia{
				Title:    "The Well of Ascension",
				Class:    audiobook.ClassAudiobook,
				Sequence: "2",
			},
			WantOK: true,
		},
		{
			Name: "folder layout: fractional series sequence from grandparent",
			Path: "/lib/audiobooks/Author/Series 4.5/Novella/book.m4b",
			Probe: sdk.ParseProbe{
				Tags: map[string]string{},
			},
			Want: sdk.ParsedMedia{
				Title:    "Novella",
				Class:    audiobook.ClassAudiobook,
				Sequence: "4.5",
			},
			WantOK: true,
		},
		{
			// Untagged flat-under-author layout: leaf folder "Frank Herbert" is
			// the author, but with no tags it is indistinguishable from a Title
			// folder (cf. "The Well of Ascension" above), so the leaf folder wins
			// as the title — a documented single-file ambiguity, not a bug. An
			// author tag resolves it (see the album_artist case below). Leading
			// ordinal "01" sets Index=1 and PartGroup to dir.
			Name: "untagged flat layout: leaf folder wins (inherent ambiguity)",
			Path: "/lib/audiobooks/Frank Herbert/01 - Dune.mp3",
			Probe: sdk.ParseProbe{
				Tags: map[string]string{},
			},
			Want: sdk.ParsedMedia{
				Title:     "Frank Herbert",
				Class:     audiobook.ClassAudiobook,
				Index:     1,
				PartGroup: "/lib/audiobooks/Frank Herbert",
			},
			WantOK: true,
		},
		{
			// Flat-under-author layout disambiguated by an author tag: the
			// album_artist tag equals the leaf folder, proving the folder is the
			// author and the title lives in the filename. Without this the book
			// would be mis-titled "Brandon Sanderson" (a bug found by
			// cross-plugin conformance testing).
			Name: "author folder detected via album_artist: title from stem",
			Path: "/lib/audiobooks/Brandon Sanderson/Elantris.m4b",
			Probe: sdk.ParseProbe{
				Tags: map[string]string{"album_artist": "Brandon Sanderson"},
			},
			Want: sdk.ParsedMedia{
				Title: "Elantris",
				Class: audiobook.ClassAudiobook,
			},
			WantOK: true,
		},
		{
			// Stem fallback: no album tag, leaf folder is "." (root-relative path),
			// so cleanName returns "" and the parser falls through to the file stem.
			// stripLeadingOrdinal removes the "02 - " prefix → "Elantris" is the
			// title. Stripping only shapes the title: the part-ordinal detection
			// re-matches the raw stem, so the same "02" also sets Index=2 and
			// PartGroup.
			Name: "stem fallback when leaf folder is root-relative dot",
			Path: "02 - Elantris.m4b",
			Probe: sdk.ParseProbe{
				Tags: map[string]string{},
			},
			Want: sdk.ParsedMedia{
				Title:     "Elantris",
				Class:     audiobook.ClassAudiobook,
				Index:     2,
				PartGroup: ".",
			},
			WantOK: true,
		},
		{
			// Regression (rune safety): an ordinal keyword may carry a rune whose
			// lowercase form has a DIFFERENT byte length — U+212A KELVIN SIGN
			// (3 bytes) lowers to ASCII "k" (1 byte), so the 7-byte "TracK"
			// lowercases to the 5-byte "track". stripLeadingOrdinal used to find
			// the match on a lowercased copy and slice the original at the copy's
			// byte offsets, garbling the stem-fallback title to "05 - Intro".
			// Case folding now happens inside the regex ((?i)) against the
			// original string, so the offsets line up and the prefix strips.
			Name: "kelvin-sign keyword ordinal: case-folded strip without byte drift",
			Path: "TracK 05 - Intro.mp3",
			Probe: sdk.ParseProbe{
				Tags: map[string]string{},
			},
			Want: sdk.ParsedMedia{
				Title:     "Intro",
				Class:     audiobook.ClassAudiobook,
				Index:     5,
				PartGroup: ".",
			},
			WantOK: true,
		},
		{
			// No title derivable: no album tag, leaf folder is ".", file stem after
			// stripping ordinal is empty. Parser returns ok=false.
			Name: "no title derivable: returns false",
			Path: ".",
			Probe: sdk.ParseProbe{
				Tags: map[string]string{},
			},
			Want:   sdk.ParsedMedia{},
			WantOK: false,
		},
		{
			// Characterization: whitespace-only album tag. strings.TrimSpace
			// reduces it to "", so Title falls through the same path as no album
			// tag at all — the leaf folder ("Title") wins. Pins that a
			// whitespace-only tag is treated as absent, not as a blank title.
			Name: "whitespace-only album tag: treated as absent, folder wins",
			Path: "/lib/audiobooks/Author/Title/01 - Chapter.m4b",
			Probe: sdk.ParseProbe{
				Tags: map[string]string{"album": "   "},
			},
			Want: sdk.ParsedMedia{
				Title:     "Title",
				Class:     audiobook.ClassAudiobook,
				Index:     1,
				PartGroup: "/lib/audiobooks/Author/Title",
			},
			WantOK: true,
		},
		{
			Name: "four-digit underscore filename ordinal sets part index",
			Path: "/lib/audiobooks/Author/Title/0001_Opening.m4b",
			Probe: sdk.ParseProbe{
				Tags: map[string]string{},
			},
			Want: sdk.ParsedMedia{
				Title:     "Title",
				Class:     audiobook.ClassAudiobook,
				Index:     1,
				PartGroup: "/lib/audiobooks/Author/Title",
			},
			WantOK: true,
		},
		{
			// A zero ordinal is not a valid part number — treated as absent so it
			// never clamps into a bogus Parts group at ordinal 0. The filename
			// here carries no leading ordinal either, so Index/IndexEnd/PartGroup
			// all stay unset.
			Name: "track tag \"0/12\": zero ordinal treated as absent, no PartGroup",
			Path: "/lib/audiobooks/Author/Title/chapter.m4b",
			Probe: sdk.ParseProbe{
				Tags: map[string]string{"album": "Title", "track": "0/12"},
			},
			Want: sdk.ParsedMedia{
				Title: "Title",
				Class: audiobook.ClassAudiobook,
			},
			WantOK: true,
		},
		{
			// A negative ordinal is likewise not a valid part number — treated as
			// absent rather than clamped into Index=-1.
			Name: "track tag \"-1/12\": negative ordinal treated as absent, no PartGroup",
			Path: "/lib/audiobooks/Author/Title/chapter.m4b",
			Probe: sdk.ParseProbe{
				Tags: map[string]string{"album": "Title", "track": "-1/12"},
			},
			Want: sdk.ParsedMedia{
				Title: "Title",
				Class: audiobook.ClassAudiobook,
			},
			WantOK: true,
		},
		{
			// A non-positive tag ordinal must not block the filename fallback:
			// "0" on the tag is treated as absent, so the leading "07" in the
			// filename still sets Index/PartGroup.
			Name: "non-positive track tag permits filename ordinal fallback",
			Path: "/lib/audiobooks/Author/Title/07 - Chapter Seven.m4b",
			Probe: sdk.ParseProbe{
				Tags: map[string]string{"album": "Title", "track": "0"},
			},
			Want: sdk.ParsedMedia{
				Title:     "Title",
				Class:     audiobook.ClassAudiobook,
				Index:     7,
				PartGroup: "/lib/audiobooks/Author/Title",
			},
			WantOK: true,
		},
		{
			// Characterization: track tag "/12" (missing numerator, only a
			// denominator). trackNumber splits on '/' and returns the substring
			// before it, which is "" — the tag block short-circuits on
			// track == "", so this behaves identically to no track tag: no
			// Index/PartGroup.
			Name: "track tag \"/12\": missing numerator yields no Index/PartGroup",
			Path: "/lib/audiobooks/Author/Title/chapter.m4b",
			Probe: sdk.ParseProbe{
				Tags: map[string]string{"album": "Title", "track": "/12"},
			},
			Want: sdk.ParsedMedia{
				Title: "Title",
				Class: audiobook.ClassAudiobook,
			},
			WantOK: true,
		},
		{
			// Characterization: grandparent (series) folder with no digits at
			// all ("NoDigitsHere"). reSeriesFolder requires trailing digits, so
			// a purely alphabetic segment never matches — Sequence stays empty,
			// same as a book with no series folder.
			Name: "no-digit grandparent folder: no series match, Sequence stays empty",
			Path: "/lib/audiobooks/Brandon Sanderson/NoDigitsHere/The Well of Ascension/book.m4b",
			Probe: sdk.ParseProbe{
				Tags: map[string]string{},
			},
			Want: sdk.ParsedMedia{
				Title: "The Well of Ascension",
				Class: audiobook.ClassAudiobook,
			},
			WantOK: true,
		},

		// --- Known heuristic limitations (digit-containing titles) ---
		// These vectors PIN the current bare-numeric heuristic behavior on
		// legitimate titles that begin with, or whose series folder ends with,
		// digits. The behavior below is a KNOWN LIMITATION, NOT an endorsement:
		// the bare-ordinal and series-folder regexes cannot lexically tell a
		// real part/series number from digits that are part of a title or a
		// year. Every refinement that suppresses these false positives also
		// suppresses a pinned/dominant convention (bare "01 - Chapter One"
		// chapter files, bare "Mistborn 2" / "Series 4.5" folders), so no
		// strictly-dominating rule exists — see parse.go's "Known heuristic
		// limitations" note. If you "fix" one of these, a convention vector
		// above will red; treat that as the tradeoff, not a regression to route
		// around. Pins exist so a future change to the tradeoff is deliberate
		// and visible, not silent.
		{
			// Title-folder "100 Years of Solitude" is derived correctly, but the
			// leading "100" on the filename is mis-read as a part ordinal
			// (Index=100, PartGroup set). LIMITATION: bare leading digits are
			// indistinguishable from a bare-numbered chapter file ("100.mp3").
			Name: "digit-title limitation: leading number mis-read as part ordinal",
			Path: "/lib/audiobooks/Gabriel Garcia Marquez/100 Years of Solitude/100 Years of Solitude.mp3",
			Probe: sdk.ParseProbe{
				Tags: map[string]string{},
			},
			Want: sdk.ParsedMedia{
				Title:     "100 Years of Solitude",
				Class:     audiobook.ClassAudiobook,
				Index:     100,
				PartGroup: "/lib/audiobooks/Gabriel Garcia Marquez/100 Years of Solitude",
			},
			WantOK: true,
		},
		{
			// A purely numeric title ("1984") is mis-read as the part ordinal
			// Index=1984. LIMITATION: an all-digit stem is lexically identical to
			// a numbered chapter file; no rule separates them without breaking
			// bare-numbered chapters.
			Name: "digit-title limitation: all-numeric title becomes part ordinal",
			Path: "/lib/audiobooks/George Orwell/1984/1984.m4b",
			Probe: sdk.ParseProbe{
				Tags: map[string]string{},
			},
			Want: sdk.ParsedMedia{
				Title:     "1984",
				Class:     audiobook.ClassAudiobook,
				Index:     1984,
				PartGroup: "/lib/audiobooks/George Orwell/1984",
			},
			WantOK: true,
		},
		{
			// Foil / boundary case: "Catch-22" has its digits mid-title (after
			// "Catch-"), not leading, so reLeadingOrdinal does NOT match and no
			// spurious Index is set. Pins that the limitation is specific to
			// LEADING digits — embedded digits are safe — so a future refinement
			// that touches leading digits must not regress this.
			Name: "digit-title foil: embedded (non-leading) digits set no ordinal",
			Path: "/lib/audiobooks/Joseph Heller/Catch-22/Catch-22.m4b",
			Probe: sdk.ParseProbe{
				Tags: map[string]string{},
			},
			Want: sdk.ParsedMedia{
				Title: "Catch-22",
				Class: audiobook.ClassAudiobook,
			},
			WantOK: true,
		},
		{
			// A series (grandparent) folder that ends in a year/number for
			// non-series reasons ("Live at Wembley 95") is mis-read as a series
			// position (Sequence="95"). LIMITATION: reSeriesFolder cannot tell a
			// trailing series ordinal ("Mistborn 2") from a trailing year/number
			// that is part of the name; both are "<words> <1-3 digits>".
			Name: "digit-folder limitation: trailing year mis-read as series sequence",
			Path: "/lib/audiobooks/Queen/Live at Wembley 95/Concert Film/track01.mp3",
			Probe: sdk.ParseProbe{
				Tags: map[string]string{},
			},
			Want: sdk.ParsedMedia{
				Title:     "Concert Film",
				Class:     audiobook.ClassAudiobook,
				Index:     1,
				Sequence:  "95",
				PartGroup: "/lib/audiobooks/Queen/Live at Wembley 95/Concert Film",
			},
			WantOK: true,
		},
	})
}

func TestParseWithContextConformance(t *testing.T) {
	path := "/lib/audiobooks/Author/Title/01 - Chapter.m4b"
	tagged := sdk.ParseProbe{Tags: map[string]string{"album": "Title", "track": "1"}}
	parsed := sdk.ParsedMedia{Title: "Title", Class: audiobook.ClassAudiobook, Index: 1, PartGroup: "/lib/audiobooks/Author/Title"}
	sdktest.RunContextConformance(t, audiobook.New(), []sdktest.ContextParseVector{
		{Name: "unknown context preserves legacy parse", Path: path, Probe: tagged, Want: parsed, WantOK: true},
		{Name: "single file preserves path fallback", Path: path, Context: sdk.ParseContext{DirectoryMediaFileCount: sdk.DirectoryMediaFileCountSingle}, Want: parsed, WantOK: true},
		{Name: "tagged multi-file grouping is safe", Path: path, Probe: tagged, Context: sdk.ParseContext{DirectoryMediaFileCount: sdk.DirectoryMediaFileCountMultiple}, Want: parsed, WantOK: true},
		{Name: "multi-file grouping without book tags is ambiguous", Path: path, Context: sdk.ParseContext{DirectoryMediaFileCount: sdk.DirectoryMediaFileCountMultiple}, Want: sdk.ParsedMedia{}, WantOK: false},
		{Name: "multi-file grouping without ordering tag is ambiguous", Path: path, Probe: sdk.ParseProbe{Tags: map[string]string{"album": "Title"}}, Context: sdk.ParseContext{DirectoryMediaFileCount: sdk.DirectoryMediaFileCountMultiple}, Want: sdk.ParsedMedia{}, WantOK: false},
		{Name: "future context preserves legacy parse", Path: path, Probe: tagged, Context: sdk.ParseContext{DirectoryMediaFileCount: sdk.DirectoryMediaFileCount("future")}, Want: parsed, WantOK: true},
	})
}

// TestBundleConformance pins the declarative bundle + locale invariants.
func TestBundleConformance(t *testing.T) {
	sdktest.RunBundleConformance(t, audiobook.Bundle())
	sdktest.RequireEntityKindVocab(t, audiobook.Bundle(), []sdktest.KindVocab{
		{Name: "person", Archetype: "person"},
		{Name: "series", Archetype: "container"},
		{Name: "genre", Archetype: "category"},
	})

	// audiobook.PluginID and Bundle().ID are two independent literals; core
	// keys the parser binding on audiobook.PluginID (app.go). A rename of
	// either literal without the other must fail loudly here, not orphan the
	// parser silently.
	if audiobook.PluginID != audiobook.Bundle().ID {
		t.Fatalf("PluginID %q != Bundle().ID %q", audiobook.PluginID, audiobook.Bundle().ID)
	}
}

// mediaFieldVector is one golden ParseMediaFields case: a probe in, the
// expected []sdk.ParsedTextField out. sdktest has no shared runner for
// sdk.MediaFieldParser (only FilenameParser/Scorer/Bundle), so this table
// pins the contract locally, mirroring sdktest.RunConformance's shape.
type mediaFieldVector struct {
	Name  string
	Probe sdk.ParseProbe
	Want  []sdk.ParsedTextField
}

// TestParseMediaFieldsConformance pins ParseMediaFields — the sole
// narrator-extraction path: its own sdk.MediaFieldParser interface, its own
// narrator > composer priority order (artist is deliberately excluded), and the Filterable:true
// "narrator" field it feeds (bundle.go). This was the zero-coverage gap that
// let the artist-as-narrator bug ship (a book with an "artist" tag but no
// "narrator"/"composer" tag was silently attributed to the wrong person);
// the last two vectors pin that exact narrator-vs-artist distinction as a
// regression guard.
func TestParseMediaFieldsConformance(t *testing.T) {
	p := audiobook.New()
	vectors := []mediaFieldVector{
		{
			Name:  "narrator tag present: wins outright",
			Probe: sdk.ParseProbe{Tags: map[string]string{"narrator": "Michael Kramer"}},
			Want:  []sdk.ParsedTextField{{Name: "narrator", Text: "Michael Kramer"}},
		},
		{
			Name: "narrator beats composer and artist when all present",
			Probe: sdk.ParseProbe{Tags: map[string]string{
				"narrator": "Michael Kramer",
				"composer": "Some Composer",
				"artist":   "Some Artist",
			}},
			Want: []sdk.ParsedTextField{{Name: "narrator", Text: "Michael Kramer"}},
		},
		{
			Name: "no narrator tag: composer is the fallback narrator source",
			Probe: sdk.ParseProbe{Tags: map[string]string{
				"composer": "Some Composer",
				"artist":   "Some Artist",
			}},
			Want: []sdk.ParsedTextField{{Name: "narrator", Text: "Some Composer"}},
		},
		{
			// Regression: no narrator or composer tag,
			// only artist. This is the most common rip layout and previously let
			// the tagged "artist" (often the book's author) get silently
			// attributed as the narrator. artist is semantically the
			// performer/author, never the narrator, so it must not be in the
			// fallback chain at all — an artist-only probe yields no narrator
			// field.
			Name:  "no narrator or composer tag: artist alone is not a narrator source",
			Probe: sdk.ParseProbe{Tags: map[string]string{"artist": "Some Artist"}},
			Want:  nil,
		},
		{
			// Regression: an album_artist (author) tag alone must NOT be picked
			// up as narrator — ParseMediaFields reads only
			// narrator/composer/artist, never album_artist. This pins the
			// artist-vs-narrator distinction: author metadata must never leak
			// into the narrator field.
			Name:  "album_artist tag alone is not a narrator source",
			Probe: sdk.ParseProbe{Tags: map[string]string{"album_artist": "Brandon Sanderson"}},
			Want:  nil,
		},
		{
			Name:  "no relevant tags: nil, not empty slice",
			Probe: sdk.ParseProbe{Tags: map[string]string{}},
			Want:  nil,
		},
		{
			Name:  "nil tag map: nil",
			Probe: sdk.ParseProbe{},
			Want:  nil,
		},
		{
			Name:  "blank narrator tag with only artist present: nil, artist is not a fallback",
			Probe: sdk.ParseProbe{Tags: map[string]string{"narrator": "   ", "artist": "Some Artist"}},
			Want:  nil,
		},
	}
	for _, v := range vectors {
		v := v
		t.Run(v.Name, func(t *testing.T) {
			got := p.ParseMediaFields("/lib/audiobooks/Author/Book/file.m4b", v.Probe)
			if !reflect.DeepEqual(got, v.Want) {
				t.Errorf("ParseMediaFields(...) =\n  %+v\nwant\n  %+v", got, v.Want)
			}
			got2 := p.ParseMediaFields("/lib/audiobooks/Author/Book/file.m4b", v.Probe)
			if !reflect.DeepEqual(got2, got) {
				t.Errorf("ParseMediaFields(...) not deterministic: %+v then %+v", got, got2)
			}
		})
	}
}
