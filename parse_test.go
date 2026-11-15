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
