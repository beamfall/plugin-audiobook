package audiobook

import (
	"context"
	"errors"
	"strings"
)

// PackKindBook is the offline pack target kind for one audiobook. It is the
// opaque kind string a core adapter registers into the offline pack service.
const PackKindBook = "audiobook-book"

// ErrBookPackRef is returned for a book pack ref that does not name exactly
// one Media id.
var ErrBookPackRef = errors.New("audiobook: book pack names exactly one Media id")

// ErrBookNotVisible is returned when the named book is not visible to the
// caller.
var ErrBookNotVisible = errors.New("audiobook: book is not visible")

// PackResolved is the output of a PackResolver: the visible Media IDs that
// belong in the pack plus a manifest-safe display label. The core adapter
// translates the plain string IDs to its own Media ID type.
type PackResolved struct {
	MediaIDs []string
	Label    string
}

// PackMediaReader is the filter-bound store view the resolvers read. The core
// adapter binds the caller's Library filter, so resolvers never see it.
type PackMediaReader interface {
	// VisibleSubset returns the subset of ids that are visible, preserving order.
	VisibleSubset(ctx context.Context, ids []string) ([]string, error)
}

// PackResolver translates an opaque target ref into the visible Media that
// belong in the pack.
type PackResolver interface {
	Resolve(ctx context.Context, targetRef string, r PackMediaReader) (PackResolved, error)
}

// PackResolvers returns the plugin's offline pack resolvers keyed by kind.
func PackResolvers() map[string]PackResolver {
	return map[string]PackResolver{PackKindBook: bookResolver{}}
}

// bookResolver resolves one book. A book is ONE Media whose audio files are
// its Parts and whose chapters are Markers, so the pack names that single
// Media; core packs every Part of its selected Version and its chapters.
type bookResolver struct{}

func (bookResolver) Resolve(ctx context.Context, targetRef string, rd PackMediaReader) (PackResolved, error) {
	fields := strings.FieldsFunc(targetRef, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\n' || r == '\t'
	})
	if len(fields) != 1 {
		return PackResolved{}, ErrBookPackRef
	}
	visible, err := rd.VisibleSubset(ctx, fields)
	if err != nil {
		return PackResolved{}, err
	}
	if len(visible) != 1 || visible[0] != fields[0] {
		return PackResolved{}, ErrBookNotVisible
	}
	return PackResolved{MediaIDs: visible, Label: "Audiobook"}, nil
}
