package audiobook

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"

	sdk "github.com/beamfall/plugin-sdk"
)

func orderedDirectoryNames(t *testing.T, dir string, probes map[string]sdk.ParseProbe, hasMediaSubdirs bool) (order []string, results map[string]sdk.ParsedMedia, reason string, handled bool) {
	t.Helper()
	results, reason, handled = New().ParseDirectory(dir, probes, hasMediaSubdirs)
	if !handled || reason != "" {
		return nil, results, reason, handled
	}
	type item struct {
		name string
		idx  int
	}
	items := make([]item, 0, len(results))
	for path, parsed := range results {
		items = append(items, item{name: filepath.Base(path), idx: parsed.Index})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].idx != items[j].idx {
			return items[i].idx < items[j].idx
		}
		return items[i].name < items[j].name
	})
	for i, it := range items {
		order = append(order, it.name)
		if i > 0 && items[i-1].idx == it.idx {
			t.Fatalf("duplicate Index %d for %s and %s", it.idx, items[i-1].name, it.name)
		}
	}
	return order, results, "", true
}

func TestParseDirectoryPartialTagsAndAuthorNoiseLayouts(t *testing.T) {
	tests := []struct {
		name      string
		dir       string
		probes    map[string]sdk.ParseProbe
		wantTitle string
		wantGroup string
		wantSeq   string
		wantOrder []string
	}{
		{
			name: "partial album and track tags mixed with untagged parts",
			dir:  "/lib/Elton John/Elton John - Me (Unabridged) [MP3]",
			probes: map[string]sdk.ParseProbe{
				"/lib/Elton John/Elton John - Me (Unabridged) [MP3]/Part02.mp3": {HasAudio: true, Tags: map[string]string{"artist": "Elton John"}},
				"/lib/Elton John/Elton John - Me (Unabridged) [MP3]/Part01.mp3": {HasAudio: true, Tags: map[string]string{"album": "Me", "track": "1/3", "artist": "Elton John"}},
				"/lib/Elton John/Elton John - Me (Unabridged) [MP3]/Part10.mp3": {HasAudio: true, Tags: map[string]string{"album": "Me (Unabridged)", "artist": "Elton John"}},
			},
			wantTitle: "Me",
			wantGroup: "/lib/Elton John/Elton John - Me (Unabridged) [MP3]",
			wantOrder: []string{"Part01.mp3", "Part02.mp3", "Part10.mp3"},
		},
		{
			name: "author prefix stripped from folder using artist tag when under root",
			dir:  "/lib/Kate Moore - The Radium Girls [M4B] (Read by Angela Brazil)",
			probes: map[string]sdk.ParseProbe{
				"/lib/Kate Moore - The Radium Girls [M4B] (Read by Angela Brazil)/02 - Chapter Two.mp3": {HasAudio: true, Tags: map[string]string{"artist": "Kate Moore"}},
				"/lib/Kate Moore - The Radium Girls [M4B] (Read by Angela Brazil)/01 - Chapter One.mp3": {HasAudio: true, Tags: nil},
				"/lib/Kate Moore - The Radium Girls [M4B] (Read by Angela Brazil)/Credits.mp3":          {HasAudio: true, Tags: nil},
			},
			wantTitle: "The Radium Girls",
			wantGroup: "/lib/Kate Moore - The Radium Girls [M4B] (Read by Angela Brazil)",
			wantOrder: []string{"01 - Chapter One.mp3", "02 - Chapter Two.mp3", "Credits.mp3"},
		},
		{
			name: "author prefix stripped via parent folder when untagged",
			dir:  "/lib/Neil Gaiman/Sandman 01/Neil Gaiman - American Gods (Unabridged)",
			probes: map[string]sdk.ParseProbe{
				"/lib/Neil Gaiman/Sandman 01/Neil Gaiman - American Gods (Unabridged)/Chapter 10.mp3": {HasAudio: true},
				"/lib/Neil Gaiman/Sandman 01/Neil Gaiman - American Gods (Unabridged)/Chapter 2.mp3":  {HasAudio: true},
				"/lib/Neil Gaiman/Sandman 01/Neil Gaiman - American Gods (Unabridged)/Chapter 1.mp3":  {HasAudio: true},
			},
			wantTitle: "American Gods",
			wantGroup: "/lib/Neil Gaiman/Sandman 01/Neil Gaiman - American Gods (Unabridged)",
			wantSeq:   "01",
			wantOrder: []string{"Chapter 1.mp3", "Chapter 2.mp3", "Chapter 10.mp3"},
		},
		{
			name: "author prefix stripped via parent when series folder is absent",
			dir:  "/lib/Neil Gaiman/Neil Gaiman - American Gods - Unabridged",
			probes: map[string]sdk.ParseProbe{
				"/lib/Neil Gaiman/Neil Gaiman - American Gods - Unabridged/00 - Prologue.mp3": {HasAudio: true},
				"/lib/Neil Gaiman/Neil Gaiman - American Gods - Unabridged/01 - Arrival.mp3":  {HasAudio: true},
			},
			wantTitle: "American Gods",
			wantGroup: "/lib/Neil Gaiman/Neil Gaiman - American Gods - Unabridged",
			wantOrder: []string{"00 - Prologue.mp3", "01 - Arrival.mp3"},
		},
		{
			name: "flat author folder derives common title from part stems",
			dir:  "/lib/Tina Fey",
			probes: map[string]sdk.ParseProbe{
				"/lib/Tina Fey/Bossypants - Part 2.mp3": {HasAudio: true, Tags: map[string]string{"artist": "Tina Fey"}},
				"/lib/Tina Fey/Bossypants - Part 1.mp3": {HasAudio: true, Tags: map[string]string{"artist": "Tina Fey"}},
			},
			wantTitle: "Bossypants",
			wantGroup: "/lib/Tina Fey",
			wantOrder: []string{"Bossypants - Part 1.mp3", "Bossypants - Part 2.mp3"},
		},
		{
			name: "flat author folder with bare numbered files still ingests as one book",
			dir:  "/lib/Flea",
			probes: map[string]sdk.ParseProbe{
				"/lib/Flea/02.mp3": {HasAudio: true, Tags: map[string]string{"album_artist": "Flea"}},
				"/lib/Flea/01.mp3": {HasAudio: true, Tags: map[string]string{"album_artist": "Flea"}},
			},
			wantTitle: "Flea",
			wantGroup: "/lib/Flea",
			wantOrder: []string{"01.mp3", "02.mp3"},
		},
		{
			name: "disc and track tags order across discs even with disc suffixes in album tag",
			dir:  "/lib/Frank Herbert/Dune (64kbps)",
			probes: map[string]sdk.ParseProbe{
				"/lib/Frank Herbert/Dune (64kbps)/c.mp3":     {HasAudio: true, Tags: map[string]string{"album": "Dune (Disc 2)", "disc": "2/2", "track": "1"}},
				"/lib/Frank Herbert/Dune (64kbps)/a.mp3":     {HasAudio: true, Tags: map[string]string{"album": "Dune (Disc 1)", "discnumber": "1", "track": "2"}},
				"/lib/Frank Herbert/Dune (64kbps)/b.mp3":     {HasAudio: true, Tags: map[string]string{"album": "Dune", "tpos": "1/2", "tracknumber": "1/2"}},
				"/lib/Frank Herbert/Dune (64kbps)/bonus.mp3": {HasAudio: true, Tags: nil},
			},
			wantTitle: "Dune",
			wantGroup: "/lib/Frank Herbert/Dune (64kbps)",
			wantOrder: []string{"b.mp3", "a.mp3", "c.mp3", "bonus.mp3"},
		},
		{
			name: "partial single-disc tag does not push untagged last file before disc 1",
			dir:  "/lib/Author/Book",
			probes: map[string]sdk.ParseProbe{
				"/lib/Author/Book/03 - End.mp3":   {HasAudio: true, Tags: nil},
				"/lib/Author/Book/01 - Start.mp3": {HasAudio: true, Tags: map[string]string{"disc": "1", "track": "1"}},
				"/lib/Author/Book/02 - Mid.mp3":   {HasAudio: true, Tags: map[string]string{"disc": "1", "track": "2"}},
			},
			wantTitle: "Book",
			wantGroup: "/lib/Author/Book",
			wantOrder: []string{"01 - Start.mp3", "02 - Mid.mp3", "03 - End.mp3"},
		},
		{
			name: "series sequence preserved with directory parsing",
			dir:  "/lib/Author/Saga 04/The Shadow Rising [MP3]",
			probes: map[string]sdk.ParseProbe{
				"/lib/Author/Saga 04/The Shadow Rising [MP3]/02.mp3": {HasAudio: true},
				"/lib/Author/Saga 04/The Shadow Rising [MP3]/01.mp3": {HasAudio: true},
			},
			wantTitle: "The Shadow Rising",
			wantGroup: "/lib/Author/Saga 04/The Shadow Rising [MP3]",
			wantSeq:   "04",
			wantOrder: []string{"01.mp3", "02.mp3"},
		},
		{
			name: "filename disc and track fallback without disc tags",
			dir:  "/lib/Author/Trilogy",
			probes: map[string]sdk.ParseProbe{
				"/lib/Author/Trilogy/Disc 2 - 01.mp3": {HasAudio: true},
				"/lib/Author/Trilogy/Disc 1 - 10.mp3": {HasAudio: true},
				"/lib/Author/Trilogy/Disc 1 - 02.mp3": {HasAudio: true},
			},
			wantTitle: "Trilogy",
			wantGroup: "/lib/Author/Trilogy",
			wantOrder: []string{"Disc 1 - 02.mp3", "Disc 1 - 10.mp3", "Disc 2 - 01.mp3"},
		},
		{
			name: "broken duplicate track tag broken naturally by filename numbers",
			dir:  "/lib/Author/SameTrackRip",
			probes: map[string]sdk.ParseProbe{
				"/lib/Author/SameTrackRip/Part 10.mp3": {HasAudio: true, Tags: map[string]string{"track": "1"}},
				"/lib/Author/SameTrackRip/Part 2.mp3":  {HasAudio: true, Tags: map[string]string{"track": "1"}},
				"/lib/Author/SameTrackRip/Part 1.mp3":  {HasAudio: true, Tags: map[string]string{"track": "1"}},
			},
			wantTitle: "SameTrackRip",
			wantGroup: "/lib/Author/SameTrackRip",
			wantOrder: []string{"Part 1.mp3", "Part 2.mp3", "Part 10.mp3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			order, results, reason, handled := orderedDirectoryNames(t, tt.dir, tt.probes, false)
			if !handled || reason != "" {
				t.Fatalf("ParseDirectory handled=%v reason=%q, want handled=true reason=\"\"", handled, reason)
			}
			if strings.Join(order, "|") != strings.Join(tt.wantOrder, "|") {
				t.Fatalf("order = %v, want %v", order, tt.wantOrder)
			}
			for path, got := range results {
				if got.Class != ClassAudiobook || got.Title != tt.wantTitle || got.PartGroup != tt.wantGroup || got.Sequence != tt.wantSeq {
					t.Fatalf("%s: got %+v, want Class=%q Title=%q PartGroup=%q Sequence=%q",
						filepath.Base(path), got, ClassAudiobook, tt.wantTitle, tt.wantGroup, tt.wantSeq)
				}
			}
		})
	}
}

func TestParseDirectoryRefusesOnlyConflictingAlbumsAndDelegatesSubdirs(t *testing.T) {
	t.Run("genuinely conflicting album tags refused with reason", func(t *testing.T) {
		dir := "/lib/audiobooks/Mixed"
		probes := map[string]sdk.ParseProbe{
			dir + "/01.mp3": {HasAudio: true, Tags: map[string]string{"album": "Dune", "track": "1"}},
			dir + "/02.mp3": {HasAudio: true, Tags: map[string]string{"album": "Neuromancer", "track": "2"}},
		}
		results, reason, handled := New().ParseDirectory(dir, probes, false)
		if !handled || results != nil || reason != RefusalConflictingAlbumTags {
			t.Fatalf("got (%v, %q, %v), want (nil, %q, true)", results, reason, handled, RefusalConflictingAlbumTags)
		}
	})

	t.Run("media subdirs delegate back to per-file context", func(t *testing.T) {
		dir := "/lib/Author/Book"
		probes := map[string]sdk.ParseProbe{
			dir + "/Credits.mp3": {HasAudio: true},
			dir + "/Notes.mp3":   {HasAudio: true},
		}
		results, reason, handled := New().ParseDirectory(dir, probes, true)
		if handled || reason != "" || results != nil {
			t.Fatalf("got (%v, %q, %v), want (nil, \"\", false)", results, reason, handled)
		}
	})

	t.Run("disc subdir with multiple files uses disc stride", func(t *testing.T) {
		dir := "/lib/Author/Novel (Unabridged)/Disc 2"
		probes := map[string]sdk.ParseProbe{
			dir + "/02.mp3": {HasAudio: true},
			dir + "/01.mp3": {HasAudio: true},
		}
		order, results, reason, handled := orderedDirectoryNames(t, dir, probes, false)
		if !handled || reason != "" {
			t.Fatalf("got reason=%q handled=%v", reason, handled)
		}
		if strings.Join(order, "|") != "01.mp3|02.mp3" {
			t.Fatalf("order = %v", order)
		}
		if got := results[dir+"/01.mp3"]; got.Index != 20001 || got.Title != "Novel" || got.PartGroup != "/lib/Author/Novel (Unabridged)" {
			t.Fatalf("01.mp3 = %+v, want Index 20001 Title Novel", got)
		}
	})
}

func TestPartMarkerTitleCleanedOrdinalStems(t *testing.T) {
	parser := New()
	tests := []struct {
		path string
		tags map[string]string
		want string
	}{
		{"/lib/Book/01.mp3", nil, "Part 1"},
		{"/lib/Book/Part01.mp3", nil, "Part 1"},
		{"/lib/Book/Part_02.mp3", nil, "Part 2"},
		{"/lib/Book/Chapter_03.mp3", nil, "Chapter 3"},
		{"/lib/Book/Track 04.mp3", nil, "Track 4"},
		{"/lib/Book/01 - Opening_Credits.mp3", nil, "Opening Credits"},
		{"/lib/Book/Part01.mp3", map[string]string{"title": "Prologue: Dawn"}, "Prologue: Dawn"},
		{"/lib/Book/Disc 2/Part03.mp3", nil, "Disc 2 · Part 3"},
	}
	for _, tt := range tests {
		got := parser.PartMarkerTitle(tt.path, sdk.ParseProbe{HasAudio: true, Tags: tt.tags})
		if got != tt.want {
			t.Errorf("PartMarkerTitle(%q, %v) = %q, want %q", tt.path, tt.tags, got, tt.want)
		}
	}
}

func TestCodexReviewEdgeCases(t *testing.T) {
	parser := New()

	t.Run("disc folder title matches across single-file rescan and multi-file directory", func(t *testing.T) {
		singlePath := "/lib/Neil Gaiman/Saga 01/Neil Gaiman - Book/Disc 1/01.mp3"
		single, ok := parser.ParseWithContext(singlePath, sdk.ParseProbe{}, sdk.ParseContext{DirectoryMediaFileCount: sdk.DirectoryMediaFileCountSingle})
		if !ok {
			t.Fatalf("ParseWithContext single disc failed")
		}
		multiDir := "/lib/Neil Gaiman/Saga 01/Neil Gaiman - Book/Disc 2"
		multiMap, reason, handled := parser.ParseDirectory(multiDir, map[string]sdk.ParseProbe{
			multiDir + "/01.mp3": {HasAudio: true, Tags: map[string]string{"artist": "Neil Gaiman"}},
			multiDir + "/02.mp3": {HasAudio: true, Tags: map[string]string{"artist": "Neil Gaiman"}},
		}, false)
		if !handled || reason != "" {
			t.Fatalf("ParseDirectory handled=%v reason=%q", handled, reason)
		}
		if single.Title != "Book" || multiMap[multiDir+"/01.mp3"].Title != single.Title {
			t.Fatalf("disc title mismatch: single=%q multi=%q", single.Title, multiMap[multiDir+"/01.mp3"].Title)
		}
	})

	t.Run("album-only unnumbered file in disc folder still refused", func(t *testing.T) {
		path := "/lib/Author/Book/Disc 1/Interview.mp3"
		probe := sdk.ParseProbe{HasAudio: true, Tags: map[string]string{"album": "Book"}}
		multi := sdk.ParseContext{DirectoryMediaFileCount: sdk.DirectoryMediaFileCountMultiple}
		if _, ok := parser.ParseWithContext(path, probe, multi); ok {
			t.Fatalf("expected refusal for album-only unnumbered file in Disc folder")
		}
		if got := parser.ContextRefusalReason(path, probe, multi); got != RefusalTaggedWithoutOrdinal {
			t.Fatalf("ContextRefusalReason = %q, want %q", got, RefusalTaggedWithoutOrdinal)
		}
	})

	t.Run("bitrate suffixes in album tags do not split a folder", func(t *testing.T) {
		dir := "/lib/Author/Book (64kbps)"
		results, reason, handled := parser.ParseDirectory(dir, map[string]sdk.ParseProbe{
			dir + "/01.mp3": {HasAudio: true, Tags: map[string]string{"album": "Book (64kbps)", "track": "1"}},
			dir + "/02.mp3": {HasAudio: true, Tags: map[string]string{"album": "Book (128kbps)", "track": "2"}},
		}, false)
		if !handled || reason != "" {
			t.Fatalf("got reason=%q handled=%v", reason, handled)
		}
		if results[dir+"/01.mp3"].Title != "Book" || results[dir+"/02.mp3"].Title != "Book" {
			t.Fatalf("titles = %q / %q, want \"Book\"", results[dir+"/01.mp3"].Title, results[dir+"/02.mp3"].Title)
		}
	})

	t.Run("mixed-case tag aliases resolve deterministically", func(t *testing.T) {
		tags := map[string]string{"ALBUM": "Book A", "Album": "Book B"}
		first := tagValue(tags, "album")
		for range 50 {
			if got := tagValue(tags, "album"); got != first {
				t.Fatalf("non-deterministic tagValue: got %q then %q", first, got)
			}
		}
	})

	t.Run("naturalCompare satisfies transitivity across ASCII and non-ASCII runes", func(t *testing.T) {
		samples := []string{"Appendix ١", "Appendix 999", "Appendix 2", "Appendix 10", "Appendix Z", "Appendix 02"}
		for _, a := range samples {
			if naturalCompare(a, a) != 0 {
				t.Fatalf("irreflexive for %q", a)
			}
			for _, b := range samples {
				ab := naturalCompare(a, b)
				ba := naturalCompare(b, a)
				if ab != -ba {
					t.Fatalf("asymmetry for (%q, %q): %d vs %d", a, b, ab, ba)
				}
				for _, c := range samples {
					bc := naturalCompare(b, c)
					ac := naturalCompare(a, c)
					if ab < 0 && bc < 0 && ac >= 0 {
						t.Fatalf("transitivity violation: %q < %q < %q, but cmp(a,c)=%d", a, b, c, ac)
					}
					if ab == 0 && bc < 0 && ac >= 0 {
						t.Fatalf("equiv transitivity violation: %q == %q < %q, but cmp(a,c)=%d", a, b, c, ac)
					}
				}
			}
		}
	})
}
