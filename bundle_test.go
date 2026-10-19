package audiobook

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	sdk "github.com/beamfall/plugin-sdk"
)

func TestOpenLibraryAuthorLookupsStayBounded(t *testing.T) {
	entityMap := openLibraryAuthorEntityMap(t)
	if entityMap.MaxLookups != openLibraryMaxAuthorLookups {
		t.Fatalf("Open Library author MaxLookups = %d, want %d", entityMap.MaxLookups, openLibraryMaxAuthorLookups)
	}

	doer := &countingDoer{authorRefs: entityMap.MaxLookups + 3}
	authors := fetchOpenLibraryAnthologyAuthors(t, doer, "https://openlibrary.test", entityMap)
	wantRequests := 1 + entityMap.MaxLookups
	if doer.requests != wantRequests {
		t.Fatalf("requests = %d, want %d", doer.requests, wantRequests)
	}
	if len(authors) != entityMap.MaxLookups {
		t.Fatalf("authors = %d, want %d", len(authors), entityMap.MaxLookups)
	}
}

func TestCoverSourcesStayOfficialAndPosterCapable(t *testing.T) {
	want := []string{"itunes-audiobook", "google-books", "open-library"}
	scrapers := Bundle().Scrapers
	if len(scrapers) != len(want) {
		t.Fatalf("scrapers = %d, want %d official/open sources", len(scrapers), len(want))
	}
	for i, scraper := range scrapers {
		if scraper.ID != want[i] {
			t.Fatalf("scraper[%d] = %q, want %q", i, scraper.ID, want[i])
		}
		if scraper.ArtworkMap["poster"] == "" && scraper.SearchArtworkMap["poster"] == "" {
			t.Fatalf("scraper %q has no poster mapping", scraper.ID)
		}
		source := strings.ToLower(fmt.Sprint(
			scraper.ID, " ", scraper.DisplayName, " ", scraper.Endpoint, " ",
			scraper.SearchTemplate, " ", scraper.LookupTemplate, " ",
			scraper.ArtworkMap, " ", scraper.SearchArtworkMap, " ", scraper.ArtworkURLTemplates,
		))
		if strings.Contains(source, "audible") || strings.Contains(source, "audnexus") {
			t.Fatalf("prohibited cover source %q", scraper.ID)
		}
	}
}

type countingDoer struct {
	requests   int
	authorRefs int
}

func (d *countingDoer) Do(req *http.Request) (*http.Response, error) {
	d.requests++
	body := `{"name":"Author"}`
	if req.URL.Path == "/works/anthology.json" {
		authors := make([]map[string]map[string]string, d.authorRefs)
		for i := range authors {
			authors[i] = map[string]map[string]string{"author": {"key": fmt.Sprintf("/authors/OL%dA", i)}}
		}
		data, err := json.Marshal(map[string]any{"authors": authors})
		if err != nil {
			return nil, err
		}
		body = string(data)
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
}

func openLibraryAuthorEntityMap(t *testing.T) sdk.EntityMap {
	t.Helper()
	for _, scraper := range Bundle().Scrapers {
		if scraper.ID != "open-library" {
			continue
		}
		for _, entityMap := range scraper.EntityMaps {
			if entityMap.Kind == "person" && entityMap.Relation == "credited-on" && entityMap.LookupTemplate != "" {
				return entityMap
			}
		}
	}
	t.Fatal("Open Library author EntityMap not found")
	return sdk.EntityMap{}
}

type doer interface {
	Do(*http.Request) (*http.Response, error)
}

func fetchOpenLibraryAnthologyAuthors(t *testing.T, client doer, endpoint string, entityMap sdk.EntityMap) []string {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, endpoint+"/works/anthology.json", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	var work struct {
		Authors []struct {
			Author struct {
				Key string `json:"key"`
			} `json:"author"`
		} `json:"authors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&work); err != nil {
		t.Fatal(err)
	}

	authors := make([]string, 0, entityMap.MaxLookups)
	for i, ref := range work.Authors {
		if i >= entityMap.MaxLookups {
			break
		}
		path := strings.ReplaceAll(entityMap.LookupTemplate, "{id}", ref.Author.Key)
		req, err := http.NewRequest(http.MethodGet, endpoint+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		authors = append(authors, ref.Author.Key)
	}
	return authors
}

// wantBundleVersion is the release semver this head carries: the release step
// tags the head `v`+wantBundleVersion, so a bundle Version left behind by a
// tag bump fails here instead of shipping a stale identity.
const wantBundleVersion = "0.1.4"

// TestBundleVersionMatchesReleaseTag pins Bundle().Version to the semver of
// the release tag applied to this head.
func TestBundleVersionMatchesReleaseTag(t *testing.T) {
	if got := Bundle().Version; got != wantBundleVersion {
		t.Fatalf("Bundle().Version = %q, want %q", got, wantBundleVersion)
	}
}

// TestBundleCompatibleWithPinnedSDK pins the declared [SDKFloor, SDKCeiling]
// range against the SDK identity this module pins, so a bundle whose range
// excludes its own pinned SDK fails here rather than at core load time
// (sdk.CompatibleWithSDK).
func TestBundleCompatibleWithPinnedSDK(t *testing.T) {
	b := Bundle()
	if !sdk.CompatibleWithSDK(b, sdk.Version) {
		t.Fatalf("Bundle() compat range [%s, %s] excludes pinned SDK %s", b.SDKFloor, b.SDKCeiling, sdk.Version)
	}
}
