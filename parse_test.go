package audiobook

import (
	"sort"
	"strings"
	"testing"

	sdk "github.com/beamfall/plugin-sdk"
)

// TestStripLeadingOrdinal pins the ordinal-stripping helper directly,
// including the multi-byte-rune regression: stripLeadingOrdinal used to find
// the regex match on a strings.ToLower copy but slice the ORIGINAL string at
// the copy's byte offsets, so any rune whose lowercase form has a different
// byte length (U+212A KELVIN SIGN, 3 bytes, lowers to ASCII "k", 1 byte)
// shifted the match end and garbled the returned title. The regex now folds
// case itself ((?i)) and matches the original string.
func TestStripLeadingOrdinal(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"bare ordinal with dash", "01 - Intro", "Intro"},
		{"keyword ordinal", "Track 03 Foo", "Foo"},
		{"uppercase keyword ordinal", "TRACK 03 Foo", "Foo"},
		{"kelvin-sign keyword folds without byte drift", "Trac\u212A 05 - Intro", "Intro"},
		{"embedded digits untouched", "Catch-22", "Catch-22"},
		{"all-digit stem strips to empty", "1984", ""},
		{"no ordinal", "Elantris", "Elantris"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := stripLeadingOrdinal(tc.in); got != tc.want {
				t.Errorf("stripLeadingOrdinal(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestPartMarkerTitle(t *testing.T) {
	parser := New()
	tests := []struct {
		name  string
		path  string
		probe map[string]string
		want  string
	}{
		{
			name:  "embedded track title wins",
			path:  "/books/Novel/01 - Filename Label.mp3",
			probe: map[string]string{"title": "Tagged Chapter"},
			want:  "Tagged Chapter",
		},
		{
			name: "filename fallback strips ordinal",
			path: "/books/Novel/02 - The Door.mp3",
			want: "The Door",
		},
		{
			name: "disc folder prefixes the filename label",
			path: "/books/Novel/Disc 2/02 - The Door.mp3",
			want: "Disc 2 · The Door",
		},
		{
			name: "disc folder labels a bare-number file by disc and part",
			path: "/books/Novel/CD 02/03.mp3",
			want: "Disc 2 · Part 3",
		},
		{
			name: "disc folder reads a digit title as its part number (known limitation)",
			path: "/books/Novel/Disc 3/1984.mp3",
			want: "Disc 3 · Part 1984",
		},
		{
			name:  "disc folder keeps the embedded title",
			path:  "/books/Novel/Disc 2/03.mp3",
			probe: map[string]string{"title": "Tagged Chapter"},
			want:  "Tagged Chapter",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parser.PartMarkerTitle(tt.path, sdk.ParseProbe{Tags: tt.probe})
			if got != tt.want {
				t.Fatalf("PartMarkerTitle() = %q, want %q", got, tt.want)
			}
		})
	}
}

// multiFolder parses every name in dir with multi-file context, the way core's
// scan preflight does, and returns the accepted parts in core's order (stable
// by Index) plus the refusal reason for each refused name.
func multiFolder(t *testing.T, dir string, names []string, tags map[string]map[string]string) ([]string, map[string]string) {
	t.Helper()
	parser := New()
	context := sdk.ParseContext{DirectoryMediaFileCount: sdk.DirectoryMediaFileCountMultiple}
	type part struct {
		name   string
		parsed sdk.ParsedMedia
	}
	var parts []part
	refused := map[string]string{}
	for _, name := range names {
		path := dir + "/" + name
		probe := sdk.ParseProbe{HasAudio: true, Tags: tags[name]}
		parsed, ok := parser.ParseWithContext(path, probe, context)
		reason := parser.ContextRefusalReason(path, probe, context)
		if ok != (reason == "") {
			t.Fatalf("%s: accepted=%v but refusal reason %q", name, ok, reason)
		}
		if !ok {
			refused[name] = reason
			continue
		}
		if len(parts) > 0 && (parsed.PartGroup != parts[0].parsed.PartGroup || parsed.Title != parts[0].parsed.Title) {
			t.Fatalf("%s: grouped as %q/%q, want %q/%q", name, parsed.PartGroup, parsed.Title, parts[0].parsed.PartGroup, parts[0].parsed.Title)
		}
		parts = append(parts, part{name, parsed})
	}
	sort.SliceStable(parts, func(i, j int) bool { return parts[i].parsed.Index < parts[j].parsed.Index })
	order := make([]string, len(parts))
	for i, p := range parts {
		order[i] = p.name
	}
	return order, refused
}

func TestUntaggedNumberedPartsFolderIsOneBook(t *testing.T) {
	dir := "/lib/audiobooks/Invented Author/The Copper Lantern"
	order, refused := multiFolder(t, dir, []string{"Part 10.mp3", "01.mp3", "Part 2.mp3", "03 - The Quay.mp3"}, nil)
	if len(refused) != 0 {
		t.Fatalf("refused %v, want none", refused)
	}
	want := []string{"01.mp3", "Part 2.mp3", "03 - The Quay.mp3", "Part 10.mp3"}
	if strings.Join(order, "|") != strings.Join(want, "|") {
		t.Fatalf("part order = %v, want %v", order, want)
	}
	parsed, _ := New().ParseWithContext(dir+"/01.mp3", sdk.ParseProbe{}, sdk.ParseContext{DirectoryMediaFileCount: sdk.DirectoryMediaFileCountMultiple})
	if parsed.Title != "The Copper Lantern" || parsed.PartGroup != dir || parsed.Class != ClassAudiobook {
		t.Fatalf("parsed = %+v, want folder title and folder group", parsed)
	}
}

func TestUntaggedPartsSortNaturallyNotLexically(t *testing.T) {
	names := []string{"Part 1.mp3", "Part 10.mp3", "Part 11.mp3", "Part 2.mp3", "Part 9.mp3"}
	order, refused := multiFolder(t, "/lib/audiobooks/Invented Saga", names, nil)
	if len(refused) != 0 {
		t.Fatalf("refused %v, want none", refused)
	}
	want := []string{"Part 1.mp3", "Part 2.mp3", "Part 9.mp3", "Part 10.mp3", "Part 11.mp3"}
	if strings.Join(order, "|") != strings.Join(want, "|") {
		t.Fatalf("part order = %v, want %v", order, want)
	}
}

func TestAmbiguousMultiFileFolderStillRefusedWithReason(t *testing.T) {
	tests := []struct {
		name   string
		dir    string
		files  []string
		tags   map[string]map[string]string
		file   string
		reason string
	}{
		{
			name:   "unrelated unnumbered files in per-file context",
			dir:    "/lib/audiobooks/Mixed Downloads",
			files:  []string{"01 - Opening.mp3", "Harbour Interview.mp3", "Station Notes.mp3"},
			file:   "Harbour Interview.mp3",
			reason: RefusalUntaggedNoPartNumber,
		},
		{
			name:   "malformed ordinal tag without book",
			dir:    "/lib/audiobooks/Tagged Book",
			files:  []string{"01.mp3", "02.mp3"},
			tags:   map[string]map[string]string{"02.mp3": {"track": "/12"}},
			file:   "02.mp3",
			reason: RefusalTaggedWithoutBook,
		},
		{
			name:   "book tag without track or filename ordinal",
			dir:    "/lib/audiobooks/Tagged Book",
			files:  []string{"01.mp3", "Prologue.mp3"},
			tags:   map[string]map[string]string{"Prologue.mp3": {"album": "Tagged Book"}},
			file:   "Prologue.mp3",
			reason: RefusalTaggedWithoutOrdinal,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, refused := multiFolder(t, tt.dir, tt.files, tt.tags)
			if got := refused[tt.file]; got != tt.reason {
				t.Fatalf("refusal for %s = %q, want %q (all refused: %v)", tt.file, got, tt.reason, refused)
			}
		})
	}
}

func TestTaggedPartsFolderUnchanged(t *testing.T) {
	dir := "/lib/audiobooks/Invented Author/Folder Name"
	tags := map[string]map[string]string{
		"b.mp3": {"album": "Tagged Title", "track": "2/3"},
		"a.mp3": {"album": "Tagged Title", "track": "3/3"},
		"c.mp3": {"album": "Tagged Title", "track": "1/3"},
	}
	order, refused := multiFolder(t, dir, []string{"a.mp3", "b.mp3", "c.mp3"}, tags)
	if len(refused) != 0 {
		t.Fatalf("refused %v, want none", refused)
	}
	if strings.Join(order, "|") != "c.mp3|b.mp3|a.mp3" {
		t.Fatalf("part order = %v, want track-tag order", order)
	}
	parser := New()
	single := sdk.ParseContext{DirectoryMediaFileCount: sdk.DirectoryMediaFileCountSingle}
	if reason := parser.ContextRefusalReason(dir+"/Unnumbered.mp3", sdk.ParseProbe{}, single); reason != "" {
		t.Fatalf("single-file context refusal = %q, want none", reason)
	}
}

// TestDiscSubfoldersJoinOneBook pins CORE-3017: a book ripped into disc
// subfolders is one Media. Every disc's parts share the book folder as
// PartGroup and the book title, and core's Index sort plays them disc then
// track.
func TestDiscSubfoldersJoinOneBook(t *testing.T) {
	multi := sdk.ParseContext{DirectoryMediaFileCount: sdk.DirectoryMediaFileCountMultiple}
	single := sdk.ParseContext{DirectoryMediaFileCount: sdk.DirectoryMediaFileCountSingle}
	tests := []struct {
		name     string
		book     string
		files    []string // relative to book, in walk order
		tags     map[string]map[string]string
		context  sdk.ParseContext
		title    string
		sequence string
		order    []string
	}{
		{
			name:    "Disc N folders with bare-number files",
			book:    "/lib/audiobooks/Invented Author/The Copper Lantern",
			files:   []string{"Disc 1/01.mp3", "Disc 1/02.mp3", "Disc 10/01.mp3", "Disc 2/01.mp3", "Disc 2/02.mp3"},
			context: multi,
			title:   "The Copper Lantern",
			order:   []string{"Disc 1/01.mp3", "Disc 1/02.mp3", "Disc 2/01.mp3", "Disc 2/02.mp3", "Disc 10/01.mp3"},
		},
		{
			name:    "CD, Disk and zero-padded spellings, any case",
			book:    "/lib/audiobooks/Invented Author/The Copper Lantern",
			files:   []string{"CD1/01.mp3", "cd 2/01.mp3", "Disc 03/01.mp3", "DISK4/01.mp3", "disc_5/01.mp3"},
			context: multi,
			title:   "The Copper Lantern",
			order:   []string{"CD1/01.mp3", "cd 2/01.mp3", "Disc 03/01.mp3", "DISK4/01.mp3", "disc_5/01.mp3"},
		},
		{
			name:  "book folder titles per-disc album tags; track tags order each disc",
			book:  "/lib/audiobooks/Invented Author/Folder Name",
			files: []string{"Disc 1/b.mp3", "Disc 1/a.mp3", "Disc 2/a.mp3"},
			tags: map[string]map[string]string{
				"Disc 1/b.mp3": {"album": "Tagged Title (Disc 1)", "track": "2/2"},
				"Disc 1/a.mp3": {"album": "Tagged Title (Disc 1)", "track": "1/2"},
				"Disc 2/a.mp3": {"album": "Tagged Title (Disc 2)", "track": "1/1"},
			},
			context: multi,
			title:   "Folder Name",
			order:   []string{"Disc 1/a.mp3", "Disc 1/b.mp3", "Disc 2/a.mp3"},
		},
		{
			name:     "series sequence still read above the book folder",
			book:     "/lib/audiobooks/Invented Author/Invented Saga 04/The Copper Lantern",
			files:    []string{"Disc 2/01.mp3", "Disc 1/01.mp3"},
			context:  multi,
			title:    "The Copper Lantern",
			sequence: "04",
			order:    []string{"Disc 1/01.mp3", "Disc 2/01.mp3"},
		},
		{
			name:    "one unnumbered file per disc orders by disc",
			book:    "/lib/audiobooks/Invented Author/The Copper Lantern",
			files:   []string{"CD2/The Copper Lantern.mp3", "CD1/The Copper Lantern.mp3"},
			context: single,
			title:   "The Copper Lantern",
			order:   []string{"CD1/The Copper Lantern.mp3", "CD2/The Copper Lantern.mp3"},
		},
	}
	parser := New()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			type part struct {
				name   string
				parsed sdk.ParsedMedia
			}
			var parts []part
			for _, name := range tt.files {
				probe := sdk.ParseProbe{HasAudio: true, Tags: tt.tags[name]}
				parsed, ok := parser.ParseWithContext(tt.book+"/"+name, probe, tt.context)
				if !ok {
					t.Fatalf("%s refused: %q", name, parser.ContextRefusalReason(tt.book+"/"+name, probe, tt.context))
				}
				if parsed.PartGroup != tt.book || parsed.Title != tt.title || parsed.Sequence != tt.sequence || parsed.Index <= 0 || parsed.IndexEnd != 0 {
					t.Fatalf("%s parsed = %+v, want group %q, title %q, sequence %q, positive Index, no IndexEnd", name, parsed, tt.book, tt.title, tt.sequence)
				}
				parts = append(parts, part{name, parsed})
			}
			sort.SliceStable(parts, func(i, j int) bool { return parts[i].parsed.Index < parts[j].parsed.Index })
			order := make([]string, len(parts))
			for i, p := range parts {
				order[i] = p.name
			}
			if strings.Join(order, "|") != strings.Join(tt.order, "|") {
				t.Fatalf("part order = %v, want %v", order, tt.order)
			}
		})
	}
}

func TestDiscFolderLookalikesDoNotMerge(t *testing.T) {
	multi := sdk.ParseContext{DirectoryMediaFileCount: sdk.DirectoryMediaFileCountMultiple}
	parser := New()
	tests := []struct {
		name  string
		path  string
		group string
		title string
	}{
		{"Part folder is its own book", "/lib/Author/Saga/Part 1/01.mp3", "/lib/Author/Saga/Part 1", "Part 1"},
		{"disc word inside a title", "/lib/Author/Disc One Rising/01.mp3", "/lib/Author/Disc One Rising", "Disc One Rising"},
		{"disc with trailing words", "/lib/Author/Saga/Disc 1 Bonus/01.mp3", "/lib/Author/Saga/Disc 1 Bonus", "Disc 1 Bonus"},
		{"sibling book folders stay apart", "/lib/Author/Book B/01.mp3", "/lib/Author/Book B", "Book B"},
		{"disc zero is not a disc", "/lib/Author/Saga/Disc 0/01.mp3", "/lib/Author/Saga/Disc 0", "Disc 0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, ok := parser.ParseWithContext(tt.path, sdk.ParseProbe{}, multi)
			if !ok || parsed.PartGroup != tt.group || parsed.Title != tt.title || parsed.Index != 1 {
				t.Fatalf("parsed = %+v ok=%v, want group %q title %q Index 1", parsed, ok, tt.group, tt.title)
			}
		})
	}
}

func TestDiscFolderRefusals(t *testing.T) {
	multi := sdk.ParseContext{DirectoryMediaFileCount: sdk.DirectoryMediaFileCountMultiple}
	parser := New()
	tests := []struct {
		name   string
		path   string
		tags   map[string]string
		reason string
	}{
		{"unnumbered file beside other disc files", "/lib/Author/Title/Disc 1/Interview.mp3", nil, RefusalUntaggedNoPartNumber},
		{"track tag too large to qualify by disc", "/lib/Author/Title/Disc 1/01.mp3", map[string]string{"album": "Title", "track": "10000"}, RefusalTaggedWithoutOrdinal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			probe := sdk.ParseProbe{HasAudio: true, Tags: tt.tags}
			if _, ok := parser.ParseWithContext(tt.path, probe, multi); ok {
				t.Fatalf("accepted, want refusal %q", tt.reason)
			}
			if got := parser.ContextRefusalReason(tt.path, probe, multi); got != tt.reason {
				t.Fatalf("refusal = %q, want %q", got, tt.reason)
			}
		})
	}
}

// TestSingleFileDiscStableAcrossRescan pins that a lone file in a disc folder
// parses to the same grouping key and Index with or without its tags: core's
// rescan fast path parses an unchanged single-file folder without a probe.
func TestSingleFileDiscStableAcrossRescan(t *testing.T) {
	single := sdk.ParseContext{DirectoryMediaFileCount: sdk.DirectoryMediaFileCountSingle}
	path := "/lib/Author/Folder Name/Disc 2/03 - Finale.mp3"
	parser := New()
	tagged, okTagged := parser.ParseWithContext(path, sdk.ParseProbe{Tags: map[string]string{"album": "Novel (Disc 2)", "track": "7/9"}}, single)
	bare, okBare := parser.ParseWithContext(path, sdk.ParseProbe{}, single)
	if !okTagged || !okBare || tagged.Title != bare.Title || tagged.PartGroup != bare.PartGroup || tagged.Index != bare.Index {
		t.Fatalf("tagged = %+v, bare = %+v, want the same grouping key and Index", tagged, bare)
	}
	if bare.Index != 2*discStride || bare.Title != "Folder Name" || bare.PartGroup != "/lib/Author/Folder Name" {
		t.Fatalf("bare = %+v, want Index %d, book folder title and group", bare, 2*discStride)
	}
}

func TestDiscTitleIgnoresAuthorTags(t *testing.T) {
	single := sdk.ParseContext{DirectoryMediaFileCount: sdk.DirectoryMediaFileCountSingle}
	path := "/lib/Invented Author/Disc 2/03 - Finale.mp3"
	parsed, ok := New().ParseWithContext(path, sdk.ParseProbe{Tags: map[string]string{"album_artist": "Invented Author"}}, single)
	if !ok || parsed.Title != "Invented Author" || parsed.PartGroup != "/lib/Invented Author" {
		t.Fatalf("parsed = %+v, want the book folder title the untagged rescan also derives", parsed)
	}
}

func TestSeriesFromFolderSkipsDiscSubfolder(t *testing.T) {
	for _, path := range []string{
		"/lib/Invented Author/Invented Saga 04/The Copper Lantern/01.mp3",
		"/lib/Invented Author/Invented Saga 04/The Copper Lantern/CD 2/01.mp3",
		"/lib/Invented Author/Invented Saga 04/The Copper Lantern/Audiobook/01.mp3",
		"/lib/Invented Author/Invented Saga 04/The Copper Lantern/Audiobook/CD 2/01.mp3",
	} {
		if name, sequence := seriesFromFolder(path); name != "Invented Saga" || sequence != "04" {
			t.Fatalf("seriesFromFolder(%q) = %q, %q, want Invented Saga, 04", path, name, sequence)
		}
	}
}

func TestDiscMarkersInAlbumTagsAndFilenamesJoinOneBook(t *testing.T) {
	parser := New()
	multi := sdk.ParseContext{DirectoryMediaFileCount: sdk.DirectoryMediaFileCountMultiple}

	t.Run("Mortality album tag disc markers share one title and order by disc then track", func(t *testing.T) {
		dir := "/lib/Christopher Hitchens/Mortality"
		p1, ok1 := parser.ParseWithContext(dir+"/01.mp3", sdk.ParseProbe{
			HasAudio: true,
			Tags:     map[string]string{"album": "Mortality - Unabridged - [Disc 1]", "track": "1"},
		}, multi)
		p2, ok2 := parser.ParseWithContext(dir+"/16.mp3", sdk.ParseProbe{
			HasAudio: true,
			Tags:     map[string]string{"album": "Mortality - Unabridged - [Disc 2]", "track": "1"},
		}, multi)
		if !ok1 || !ok2 {
			t.Fatalf("expected both discs to parse, got ok1=%v ok2=%v", ok1, ok2)
		}
		if p1.Title != "Mortality" || p2.Title != "Mortality" {
			t.Fatalf("titles = %q / %q, want \"Mortality\"", p1.Title, p2.Title)
		}
		if p1.PartGroup != dir || p2.PartGroup != dir {
			t.Fatalf("PartGroup = %q / %q, want %q", p1.PartGroup, p2.PartGroup, dir)
		}
		if p1.Index != 10001 || p2.Index != 20001 {
			t.Fatalf("indices = %d / %d, want 10001 / 20001", p1.Index, p2.Index)
		}
	})

	t.Run("disc markers in filenames without disc folders", func(t *testing.T) {
		dir := "/lib/Author/Chronicle"
		cases := []struct {
			file    string
			wantIdx int
		}{
			{"01 - Chronicle [Disc 1].mp3", 10001},
			{"01 - Chronicle (Disk 3).mp3", 30001},
			{"CD1 - 02 - Chronicle.mp3", 10002},
			{"03 - Chronicle - Disc 2.mp3", 20003},
		}
		for _, tc := range cases {
			got, ok := parser.ParseWithContext(dir+"/"+tc.file, sdk.ParseProbe{HasAudio: true}, multi)
			if !ok || got.Title != "Chronicle" || got.Index != tc.wantIdx || got.PartGroup != dir {
				t.Fatalf("%s: got %+v ok=%v, want Title=Chronicle Index=%d PartGroup=%q", tc.file, got, ok, tc.wantIdx, dir)
			}
		}
	})

	t.Run("disc marker followed by noise, disc-only album, and inline filename disc", func(t *testing.T) {
		p1, ok1 := parser.ParseWithContext("/lib/Author/Book/01.mp3", sdk.ParseProbe{
			HasAudio: true,
			Tags:     map[string]string{"album": "Book - Disc 2 (Unabridged)", "track": "1"},
		}, multi)
		if !ok1 || p1.Title != "Book" || p1.Index != 20001 {
			t.Fatalf("noise after disc: got %+v ok=%v", p1, ok1)
		}
		p2, ok2 := parser.ParseWithContext("/lib/Author/Book/01.mp3", sdk.ParseProbe{
			HasAudio: true,
			Tags:     map[string]string{"album": "Disc 2", "track": "1"},
		}, multi)
		if !ok2 || p2.Title != "Book" || p2.Index != 20001 {
			t.Fatalf("disc-only album: got %+v ok=%v", p2, ok2)
		}
		p3, ok3 := parser.ParseWithContext("/lib/Author/Book Disk 1 Part 1.mp3", sdk.ParseProbe{
			HasAudio: true,
			Tags:     map[string]string{"artist": "Author"},
		}, multi)
		if !ok3 || p3.Title != "Book" || p3.Index != 10001 {
			t.Fatalf("inline filename disc: got %+v ok=%v", p3, ok3)
		}
	})
}

func TestWrapperSubfoldersFlattenedToParentBook(t *testing.T) {
	parser := New()
	single := sdk.ParseContext{DirectoryMediaFileCount: sdk.DirectoryMediaFileCountSingle}
	multi := sdk.ParseContext{DirectoryMediaFileCount: sdk.DirectoryMediaFileCountMultiple}

	cases := []struct {
		name      string
		path      string
		tags      map[string]string
		ctx       sdk.ParseContext
		wantTitle string
		wantGroup string
		wantIdx   int
	}{
		{
			name:      "A Woman First/Audiobook",
			path:      "/mnt/user/audio/books/A Woman First/Audiobook/01.mp3",
			ctx:       multi,
			wantTitle: "A Woman First",
			wantGroup: "/mnt/user/audio/books/A Woman First",
			wantIdx:   1,
		},
		{
			name:      "Chaos(Audiobook)/Chaos",
			path:      "/mnt/user/audio/books/Chaos(Audiobook)/Chaos/Chaos.m4b",
			ctx:       single,
			wantTitle: "Chaos",
		},
		{
			name:      "I, Partridge/Audiobook",
			path:      "/mnt/user/audio/books/I, Partridge/Audiobook/01.mp3",
			ctx:       multi,
			wantTitle: "I, Partridge",
			wantGroup: "/mnt/user/audio/books/I, Partridge",
			wantIdx:   1,
		},
		{
			name:      "Josh Peck/Audiobook",
			path:      "/mnt/user/audio/books/Josh Peck/Audiobook/01.mp3",
			ctx:       multi,
			wantTitle: "Josh Peck",
			wantGroup: "/mnt/user/audio/books/Josh Peck",
			wantIdx:   1,
		},
		{
			name:      "Audio/MP3/M4B wrappers",
			path:      "/lib/Author/Book Title/MP3/02.mp3",
			ctx:       multi,
			wantTitle: "Book Title",
			wantGroup: "/lib/Author/Book Title",
			wantIdx:   2,
		},
		{
			name:      "M4B wrapper",
			path:      "/lib/Author/Book Title/M4B/Book.m4b",
			ctx:       single,
			wantTitle: "Book Title",
		},
		{
			name:      "Audio wrapper with Disc subfolder",
			path:      "/lib/Author/Book Title/Audio/Disc 2/01.mp3",
			ctx:       multi,
			wantTitle: "Book Title",
			wantGroup: "/lib/Author/Book Title",
			wantIdx:   20001,
		},
		{
			name:      "Disc subfolder with MP3 wrapper inside",
			path:      "/lib/Author/Book Title/Disc 2/MP3/01.mp3",
			ctx:       multi,
			wantTitle: "Book Title",
			wantGroup: "/lib/Author/Book Title",
			wantIdx:   20001,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parser.ParseWithContext(tc.path, sdk.ParseProbe{HasAudio: true, Tags: tc.tags}, tc.ctx)
			if !ok {
				t.Fatalf("ParseWithContext(%q) ok=false", tc.path)
			}
			if got.Title != tc.wantTitle || got.PartGroup != tc.wantGroup || got.Index != tc.wantIdx {
				t.Fatalf("got %+v, want Title=%q PartGroup=%q Index=%d", got, tc.wantTitle, tc.wantGroup, tc.wantIdx)
			}
		})
	}
}
