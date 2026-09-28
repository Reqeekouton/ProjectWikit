package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
)

type recorded struct {
	mu    sync.Mutex
	paths []string
}

func (r *recorded) add(path string) {
	r.mu.Lock()
	r.paths = append(r.paths, path)
	r.mu.Unlock()
}

func (r *recorded) list() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.paths)
}

func manifestServer(t *testing.T, asked *recorded) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.add(r.URL.Path)
		switch {
		case strings.HasSuffix(r.URL.Path, "/latest.json"):
			w.Write([]byte(`{"version":"v1.2.0","published_at":"2026-09-10T00:00:00Z","packages":{}}`))
		case strings.HasSuffix(r.URL.Path, "/SHA256SUMS"):
			w.Write([]byte("abc  pwikit-v1.2.0-linux-amd64.tar.gz\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestSourceAsksTheMirrorFirst(t *testing.T) {
	var fromMirror, fromReleases recorded
	mirror := manifestServer(t, &fromMirror)
	releases := manifestServer(t, &fromReleases)

	s := Source{Releases: releases.URL + "/releases", Mirror: mirror.URL + "/projwikit/update"}
	ctx := context.Background()
	if _, err := s.Latest(ctx); err != nil {
		t.Fatalf("Latest() err = %v, want nil", err)
	}
	if _, err := s.Checksums(ctx, "v1.2.0"); err != nil {
		t.Fatalf("Checksums() err = %v, want nil", err)
	}

	want := []string{
		"/projwikit/update/latest/download/latest.json",
		"/projwikit/update/download/v1.2.0/SHA256SUMS",
	}
	if got := fromMirror.list(); !slices.Equal(got, want) {
		t.Errorf("mirror paths = %q, want %q", got, want)
	}
	if got := fromReleases.list(); len(got) != 0 {
		t.Errorf("releases paths = %q, want none", got)
	}
}

func TestSourceFallsBackWhenTheMirrorFails(t *testing.T) {
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer mirror.Close()
	var fromReleases recorded
	releases := manifestServer(t, &fromReleases)

	var notes []string
	s := Source{Releases: releases.URL + "/releases", Mirror: mirror.URL, Log: func(line string) { notes = append(notes, line) }}
	m, err := s.Latest(context.Background())
	if err != nil {
		t.Fatalf("Latest() err = %v, want nil", err)
	}
	if m.Version != "v1.2.0" {
		t.Errorf("Latest().Version = %q, want v1.2.0", m.Version)
	}
	if got, want := fromReleases.list(), []string{"/releases/latest/download/latest.json"}; !slices.Equal(got, want) {
		t.Errorf("releases paths = %q, want %q", got, want)
	}
	if len(notes) != 1 {
		t.Errorf("len(notes) = %d, want 1", len(notes))
	}
}

func TestSourceWithoutAMirrorAsksOnlyTheReleases(t *testing.T) {
	var fromReleases recorded
	releases := manifestServer(t, &fromReleases)

	s := Source{Releases: releases.URL + "/releases"}
	if _, err := s.Latest(context.Background()); err != nil {
		t.Fatalf("Latest() err = %v, want nil", err)
	}
	if got, want := fromReleases.list(), []string{"/releases/latest/download/latest.json"}; !slices.Equal(got, want) {
		t.Errorf("releases paths = %q, want %q", got, want)
	}
}
