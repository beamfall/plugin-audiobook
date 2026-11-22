package audiobook_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	audiobook "github.com/beamfall/plugin-audiobook"
)

type visibleReader map[string]bool

func (r visibleReader) VisibleSubset(_ context.Context, ids []string) ([]string, error) {
	var out []string
	for _, id := range ids {
		if r[id] {
			out = append(out, id)
		}
	}
	return out, nil
}

type failingReader struct{}

func (failingReader) VisibleSubset(context.Context, []string) ([]string, error) {
	return nil, errors.New("store down")
}

func bookResolver(t *testing.T) audiobook.PackResolver {
	t.Helper()
	resolvers := audiobook.PackResolvers()
	if len(resolvers) != 1 {
		t.Fatalf("PackResolvers = %d kinds, want 1", len(resolvers))
	}
	resolver, ok := resolvers[audiobook.PackKindBook]
	if !ok {
		t.Fatalf("PackResolvers has no %q", audiobook.PackKindBook)
	}
	return resolver
}

func TestBookPackResolvesOneVisibleMedia(t *testing.T) {
	got, err := bookResolver(t).Resolve(context.Background(), " book-1 ", visibleReader{"book-1": true})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !slices.Equal(got.MediaIDs, []string{"book-1"}) || got.Label != "Audiobook" {
		t.Fatalf("resolved = %+v, want book-1/Audiobook", got)
	}
}

func TestBookPackRefusesBadRefsAndHiddenBooks(t *testing.T) {
	reader := visibleReader{"book-1": true, "book-2": true}
	for _, tc := range []struct {
		name, ref string
		want      error
	}{
		{"empty", "  ", audiobook.ErrBookPackRef},
		{"two books", "book-1,book-2", audiobook.ErrBookPackRef},
		{"hidden", "book-3", audiobook.ErrBookNotVisible},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := bookResolver(t).Resolve(context.Background(), tc.ref, reader)
			if !errors.Is(err, tc.want) || len(got.MediaIDs) != 0 {
				t.Fatalf("Resolve(%q) = %+v, %v; want %v", tc.ref, got, err, tc.want)
			}
		})
	}
	if _, err := bookResolver(t).Resolve(context.Background(), "book-1", failingReader{}); err == nil {
		t.Fatal("Resolve with a failing reader returned no error")
	}
}
