package httpserver

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSourceOffer(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "source.tar.gz")
	data := []byte("exact build source fixture")
	if err := os.WriteFile(archive, data, 0600); err != nil {
		t.Fatal(err)
	}
	cfg := Config{SourceArchive: archive}
	if err := validateSourceOffer(cfg); err != nil {
		t.Fatal(err)
	}
	s := &Server{cfg: cfg}
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest(method, SourceArchivePath, nil))
		if w.Code != http.StatusOK || !strings.Contains(w.Header().Get("Link"), SourceArchivePath) {
			t.Fatalf("%s: %d %v", method, w.Code, w.Header())
		}
		if method == http.MethodGet && !bytes.Equal(w.Body.Bytes(), data) {
			t.Fatal("archive bytes changed")
		}
		if method == http.MethodHead && w.Body.Len() != 0 {
			t.Fatal("HEAD returned a body")
		}
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, SourceArchivePath+"?foo=1", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("unsupported query: %d", w.Code)
	}
	r := httptest.NewRequest(http.MethodGet, SourceArchivePath, nil)
	r.Header.Set("Range", "bytes=0-4")
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusPartialContent || w.Body.String() != string(data[:5]) {
		t.Fatalf("range: %d %q", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest(http.MethodPost, SourceArchivePath, nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST: %d", w.Code)
	}
}

func TestSourceOfferConfiguration(t *testing.T) {
	for _, cfg := range []Config{
		{SourceURL: "https://example.invalid/source.tar.gz"}, {},
	} {
		if err := validateSourceOffer(cfg); err != nil {
			t.Fatal(err)
		}
	}
	for _, cfg := range []Config{
		{SourceURL: "file:///private/source"}, {SourceURL: "https://user:secret@example.invalid/source"},
		{SourceURL: "https://example.invalid/source\r\nInjected: yes"},
		{SourceArchive: t.TempDir()}, {SourceArchive: filepath.Join(t.TempDir(), "missing")},
	} {
		if err := validateSourceOffer(cfg); err == nil {
			t.Fatal("invalid source offer accepted")
		}
	}
	w := httptest.NewRecorder()
	s := &Server{cfg: Config{SourceURL: "https://example.invalid/exact-source.tar.gz"}}
	s.setSourceOffer(w)
	if !strings.Contains(w.Header().Get("Link"), s.cfg.SourceURL) {
		t.Fatal("external source offer missing")
	}
}
