package audiobook

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	sdk "github.com/beamfall/plugin-sdk"
	"github.com/beamfall/plugin-sdk/sdktest"
)

func TestAudiobookshelfFixtureMapsWatchStateAndIsRerunStable(t *testing.T) {
	fixture := []byte(`{
		"libraryItems": [
			{"id": "book-resume", "path": "/library/Resume Book.m4b"}
		],
		"mediaProgress": [
			{
				"libraryItemId": "book-resume",
				"currentTime": 91.9,
				"duration": 3600.8,
				"isFinished": false,
				"lastUpdate": 1712345678000
			},
			{
				"mediaItemId": "book-complete",
				"mediaPath": "/library/Complete Book.m4b",
				"currentTime": 7200,
				"duration": 7200,
				"isFinished": true,
				"lastUpdate": "2026-07-10T12:34:56Z"
			}
		]
	}`)

	path := filepath.Join(t.TempDir(), "audiobookshelf-export.json")
	if err := os.WriteFile(path, fixture, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	source := NewAudiobookshelfSource(path)

	first, err := source.Items(context.Background()) //nolint:staticcheck // deliberate SDK 0.x compatibility assertion
	if err != nil {
		t.Fatalf("first import: %v", err)
	}
	if len(first) != 2 {
		t.Fatalf("first import items = %d, want 2", len(first))
	}
	streamed, err := collectAudiobookshelfStream(source)
	if err != nil {
		t.Fatalf("stream import: %v", err)
	}
	if !reflect.DeepEqual(streamed, first) {
		t.Fatalf("streamed items differ from compatibility Items:\nstream: %#v\nitems:  %#v", streamed, first)
	}

	resume := first[0]
	if resume.Path != "/library/Resume Book.m4b" {
		t.Fatalf("resume path = %q", resume.Path)
	}
	if len(resume.ExternalIDs) != 1 || resume.ExternalIDs[0].Scheme != audiobookshelfIDScheme || resume.ExternalIDs[0].Value != "book-resume" {
		t.Fatalf("resume external IDs = %#v", resume.ExternalIDs)
	}
	if !resume.WatchState.Present || resume.WatchState.PositionSeconds != 91 || resume.WatchState.DurationSeconds != 3600 || resume.WatchState.Watched {
		t.Fatalf("resume watch state = %#v", resume.WatchState)
	}
	if want := time.UnixMilli(1712345678000); !resume.WatchState.OccurredAt.Equal(want) {
		t.Fatalf("resume occurred at = %v, want %v", resume.WatchState.OccurredAt, want)
	}

	complete := first[1]
	if complete.Path != "/library/Complete Book.m4b" {
		t.Fatalf("complete path = %q", complete.Path)
	}
	if len(complete.ExternalIDs) != 1 || complete.ExternalIDs[0].Scheme != audiobookshelfIDScheme || complete.ExternalIDs[0].Value != "book-complete" {
		t.Fatalf("complete external IDs = %#v", complete.ExternalIDs)
	}
	if !complete.WatchState.Present || complete.WatchState.PositionSeconds != 7200 || complete.WatchState.DurationSeconds != 7200 || !complete.WatchState.Watched {
		t.Fatalf("complete watch state = %#v", complete.WatchState)
	}
	if want := time.Date(2026, 7, 10, 12, 34, 56, 0, time.UTC); !complete.WatchState.OccurredAt.Equal(want) {
		t.Fatalf("complete occurred at = %v, want %v", complete.WatchState.OccurredAt, want)
	}

	second, err := source.Items(context.Background()) //nolint:staticcheck // deliberate SDK 0.x compatibility assertion
	if err != nil {
		t.Fatalf("rerun import: %v", err)
	}
	if !reflect.DeepEqual(second, first) {
		t.Fatalf("rerun changed imported items:\nfirst:  %#v\nsecond: %#v", first, second)
	}
}

func collectAudiobookshelfStream(source sdk.Source) ([]sdk.ForeignItem, error) { //nolint:staticcheck // deliberate SDK 0.x compatibility assertion
	streaming := source.(interface {
		Stream(context.Context, string, func(context.Context, []sdk.ForeignItem, string) error) error
	})
	items := []sdk.ForeignItem{}
	err := streaming.Stream(context.Background(), "", func(
		_ context.Context,
		page []sdk.ForeignItem,
		_ string,
	) error {
		items = append(items, page...)
		return nil
	})
	return items, err
}

func TestAudiobookshelfStreamMatchesCompatibilityForNullShapes(t *testing.T) {
	for _, fixture := range []string{
		`null`,
		`{"libraryItems":null,"items":null,"mediaProgress":null,"user":null,"users":null}`,
	} {
		path := filepath.Join(t.TempDir(), "audiobookshelf-export.json")
		if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
		source := NewAudiobookshelfSource(path)
		items, err := source.Items(context.Background()) //nolint:staticcheck // deliberate SDK 0.x compatibility assertion
		if err != nil {
			t.Fatalf("compatibility Items: %v", err)
		}
		streamed, err := collectAudiobookshelfStream(source)
		if err != nil {
			t.Fatalf("stream: %v", err)
		}
		if !reflect.DeepEqual(streamed, items) {
			t.Fatalf("streamed items differ from compatibility Items: stream=%#v items=%#v", streamed, items)
		}
	}
}

func TestAudiobookshelfStreamMatchesCompatibilityForUserShapes(t *testing.T) {
	for _, fixture := range []string{
		`{"items":[{"id":"one","path":"/one.m4b"}],"user":{"mediaProgress":[{"libraryItemId":"one","currentTime":7}]}}`,
		`{"items":[{"id":"one","path":"/one.m4b"}],"users":[{"mediaProgress":[{"libraryItemId":"one","currentTime":7}]}]}`,
	} {
		path := filepath.Join(t.TempDir(), "audiobookshelf-export.json")
		if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
		source := NewAudiobookshelfSource(path)
		items, err := source.Items(context.Background()) //nolint:staticcheck // deliberate SDK 0.x compatibility assertion
		if err != nil {
			t.Fatalf("compatibility Items: %v", err)
		}
		streamed, err := collectAudiobookshelfStream(source)
		if err != nil {
			t.Fatalf("stream: %v", err)
		}
		if !reflect.DeepEqual(streamed, items) {
			t.Fatalf("streamed items differ from compatibility Items: stream=%#v items=%#v", streamed, items)
		}
	}
}

func TestAudiobookshelfStreamRejectsMalformedShapes(t *testing.T) {
	fixtures := []string{
		`1`,
		`{"libraryItems":{}}`,
		`{"users":{}}`,
		`{"users":[1]}`,
		`{"users":[{"mediaProgress":{}}]}`,
		`{"mediaProgress":{}}`,
		`{"user":[]}`,
		`[1]`,
		`[] []`,
	}
	for _, fixture := range fixtures {
		path := filepath.Join(t.TempDir(), "audiobookshelf-export.json")
		if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
		if _, err := collectAudiobookshelfStream(NewAudiobookshelfSource(path)); err == nil {
			t.Errorf("Stream(%s) returned nil error", fixture)
		}
	}
}

func TestAudiobookshelfStreamRejectsMultipleProgressUsers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audiobookshelf-export.json")
	fixture := []byte(`{"users":[{"mediaProgress":[{}]},{"mediaProgress":[{}]}]}`)
	if err := os.WriteFile(path, fixture, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if _, err := collectAudiobookshelfStream(NewAudiobookshelfSource(path)); err == nil {
		t.Fatal("Stream returned nil error for multiple progress-bearing users")
	}
}

func TestAudiobookshelfImporterStreamingCancellationAndCheckpoint(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audiobookshelf-export.json")
	progress := make([]audiobookshelfProgress, 513)
	wantItems := make([]sdk.ForeignItem, len(progress))
	for i := range progress {
		id := fmt.Sprintf("book-%03d", i)
		path := fmt.Sprintf("/%03d.m4b", i)
		progress[i] = audiobookshelfProgress{LibraryItemID: id, Path: path, CurrentTime: float64(i)}
		wantItems[i] = sdk.ForeignItem{
			Path:        path,
			ExternalIDs: []sdk.ExternalID{{Scheme: audiobookshelfIDScheme, Value: id}},
			WatchState:  sdk.WatchState{Present: true, PositionSeconds: i},
		}
	}
	fixture, err := json.Marshal(progress)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	if err := os.WriteFile(path, fixture, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	source := NewAudiobookshelfSource(path)
	streamed, err := collectAudiobookshelfStream(source)
	if err != nil {
		t.Fatalf("stream fixture: %v", err)
	}
	if !reflect.DeepEqual(streamed, wantItems) {
		t.Fatalf("streamed items differ from want: got %d items, want %d", len(streamed), len(wantItems))
	}
	streaming := source.(interface {
		Stream(context.Context, string, func(context.Context, []sdk.ForeignItem, string) error) error
	})
	ctx, cancel := context.WithCancel(context.Background())
	resumed := []sdk.ForeignItem{}
	err = streaming.Stream(ctx, "256", func(_ context.Context, page []sdk.ForeignItem, _ string) error {
		resumed = append(resumed, page...)
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled resume error = %v, want context.Canceled", err)
	}
	if !reflect.DeepEqual(resumed, wantItems[256:512]) {
		t.Fatalf("resumed page differs: got %d items, want 256", len(resumed))
	}
	callbackErr := errors.New("stop after durable page")
	err = streaming.Stream(context.Background(), "512", func(context.Context, []sdk.ForeignItem, string) error {
		return callbackErr
	})
	if !errors.Is(err, callbackErr) {
		t.Fatalf("callback error = %v, want %v", err, callbackErr)
	}
	for _, checkpoint := range []string{"bad", "514"} {
		if err := streaming.Stream(context.Background(), checkpoint, func(context.Context, []sdk.ForeignItem, string) error {
			return nil
		}); err == nil {
			t.Errorf("checkpoint %q returned nil error", checkpoint)
		}
	}
	sdktest.RunImporterConformance(t, []sdktest.ImporterVector{{
		Name:           AudiobookshelfImporterID,
		Source:         source,
		WantCategories: []string{sdk.CategoryWatchState},
		WantItems:      wantItems,
	}})
}
