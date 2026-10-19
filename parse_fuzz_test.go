package audiobook

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	sdk "github.com/beamfall/plugin-sdk"
)

func FuzzParseFilename(f *testing.F) {
	for _, seed := range []struct{ path, album, track, title string }{
		{"/books/Author/Series 2/Book/01 - Opening.m4b", "", "", "Opening"},
		{"/books/Author/Book.m4b", "Tagged Book", "1/12", "Chapter One"},
		{"", "", "", ""},
	} {
		f.Add(seed.path, seed.album, seed.track, seed.title)
	}

	parser := New()
	f.Fuzz(func(t *testing.T, path, album, track, title string) {
		if len(path)+len(album)+len(track)+len(title) > 64<<10 {
			return
		}
		probe := sdk.ParseProbe{Tags: map[string]string{"album": album, "track": track, "title": title}}
		got, ok := parser.Parse(path, probe)
		again, againOK := parser.Parse(path, probe)
		if ok != againOK || !reflect.DeepEqual(got, again) {
			t.Fatalf("Parse is not deterministic: (%+v, %v) then (%+v, %v)", got, ok, again, againOK)
		}
		if !ok {
			if !reflect.DeepEqual(got, sdk.ParsedMedia{}) {
				t.Fatalf("Parse refusal returned data: %+v", got)
			}
			return
		}
		if got.Title == "" || got.Title != strings.TrimSpace(got.Title) || got.Class != ClassAudiobook {
			t.Fatalf("Parse returned invalid identity: %+v", got)
		}
		if got.Index < 0 || got.IndexEnd < 0 || (got.PartGroup != "" && got.Index == 0) {
			t.Fatalf("Parse returned invalid part coordinates: %+v", got)
		}
	})
}

func FuzzParseAudiobookshelfExport(f *testing.F) {
	for _, seed := range []string{
		`[{"libraryItemId":"book","path":"/book.m4b","currentTime":12.5,"duration":60}]`,
		`{"mediaProgress":[],"libraryItems":[]}`,
		``,
	} {
		f.Add([]byte(seed))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		got, err := ParseAudiobookshelfExport(bytes.NewReader(data))
		again, againErr := ParseAudiobookshelfExport(bytes.NewReader(data))
		if errorText(err) != errorText(againErr) || !reflect.DeepEqual(got, again) {
			t.Fatalf("ParseAudiobookshelfExport is not deterministic: (%+v, %v) then (%+v, %v)", got, err, again, againErr)
		}
		if err != nil {
			if got != nil {
				t.Fatalf("parser returned items with error: %+v, %v", got, err)
			}
			return
		}
		if len(got) > len(data)+1 {
			t.Fatalf("parser expanded %d bytes into %d items", len(data), len(got))
		}
		for _, item := range got {
			if !item.WatchState.Present || item.WatchState.PositionSeconds < 0 || item.WatchState.DurationSeconds < 0 {
				t.Fatalf("parser returned invalid watch state: %+v", item.WatchState)
			}
		}
	})
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
