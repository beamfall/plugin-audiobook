package audiobook

import (
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
