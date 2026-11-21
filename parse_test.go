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
			name:   "unrelated unnumbered files",
			dir:    "/lib/audiobooks/Mixed Downloads",
			files:  []string{"01 - Opening.mp3", "Harbour Interview.mp3", "Station Notes.mp3"},
			file:   "Harbour Interview.mp3",
			reason: RefusalUntaggedNoPartNumber,
		},
		{
			name:   "flat author folder titles each file separately",
			dir:    "/lib/audiobooks/Invented Author",
			files:  []string{"01 First Story.mp3", "02 Second Story.mp3"},
			tags:   map[string]map[string]string{"01 First Story.mp3": {"artist": "Invented Author"}, "02 Second Story.mp3": {"artist": "Invented Author"}},
			file:   "01 First Story.mp3",
			reason: RefusalFolderNotBookTitle,
		},
		{
			name:   "author folder with bare-number files",
			dir:    "/lib/audiobooks/Invented Author",
			files:  []string{"01.mp3", "02.mp3"},
			tags:   map[string]map[string]string{"01.mp3": {"artist": "Invented Author"}, "02.mp3": {"artist": "Invented Author"}},
			file:   "01.mp3",
			reason: RefusalFolderNotBookTitle,
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
			name:   "book tag without ordinal",
			dir:    "/lib/audiobooks/Tagged Book",
			files:  []string{"01.mp3", "02.mp3"},
			tags:   map[string]map[string]string{"02.mp3": {"album": "Tagged Book"}},
			file:   "02.mp3",
			reason: RefusalTaggedWithoutOrdinal,
		},
		{
			name:   "ordinal tag without book",
			dir:    "/lib/audiobooks/Tagged Book",
			files:  []string{"01.mp3", "02.mp3"},
			tags:   map[string]map[string]string{"02.mp3": {"track": "2"}},
			file:   "02.mp3",
			reason: RefusalTaggedWithoutBook,
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
			name:  "album tag titles the book and track tags order each disc",
			book:  "/lib/audiobooks/Invented Author/Folder Name",
			files: []string{"Disc 1/b.mp3", "Disc 1/a.mp3", "Disc 2/a.mp3"},
			tags: map[string]map[string]string{
				"Disc 1/b.mp3": {"album": "Tagged Title", "track": "2/2"},
				"Disc 1/a.mp3": {"album": "Tagged Title", "track": "1/2"},
				"Disc 2/a.mp3": {"album": "Tagged Title", "track": "1/1"},
			},
			context: multi,
			title:   "Tagged Title",
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
		{"author tag names the book folder", "/lib/Author/Disc 1/01.mp3", map[string]string{"artist": "Author"}, RefusalFolderNotBookTitle},
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
