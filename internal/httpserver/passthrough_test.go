package httpserver

import (
	"bytes"
	"context"
	"image"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/vndroid/iXoSS/internal/cache"
	"github.com/vndroid/iXoSS/internal/imagetype"
)

func TestMatchingRequestsBypassProcessingAndCache(t *testing.T) {
	root := t.TempDir()
	original := testJPEG(t, 700, 500, 0)
	if err := os.WriteFile(filepath.Join(root, "photo.jpg"), original, 0o600); err != nil {
		t.Fatal(err)
	}
	// A nil processor guarantees these requests cannot quietly re-encode.
	s := newLimitedServer(t, root, 1<<20)
	cacheDir := t.TempDir()
	disk, err := cache.NewDisk(cacheDir)
	if err != nil {
		t.Fatal(err)
	}
	s.cache = disk
	info, err := s.src.Stat(context.Background(), "/photo.jpg")
	if err != nil {
		t.Fatal(err)
	}
	key := s.cacheKey(info, "image/resize,w_700\x00")
	stale := []byte("old transformed result")
	if err := disk.Put(key, stale); err != nil {
		t.Fatal(err)
	}
	for _, chain := range []string{
		"image/resize,w_700", "image/resize,h_500", "image/resize,w_700,h_500",
		"image/resize,p_100", "image/resize,w_1400", "image/format,jpg",
		"image/resize,w_700/format,jpg", "image/resize,w_700/format,auto",
	} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			r := httptest.NewRequest(method, "/photo.jpg?x-oss-process="+chain, nil)
			r.Header.Set("Accept", "image/jpeg")
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			if w.Code != http.StatusOK || w.Header().Get("X-IXoSS-Cache") != "BYPASS" || w.Header().Get("Content-Length") != strconv.Itoa(len(original)) {
				t.Fatalf("%s %s: status=%d headers=%v body=%s", method, chain, w.Code, w.Header(), w.Body)
			}
			if method == http.MethodGet && !bytes.Equal(w.Body.Bytes(), original) {
				t.Fatal("passthrough must preserve the exact original bytes")
			}
			if method == http.MethodHead && w.Body.Len() != 0 {
				t.Fatal("HEAD returned image bytes")
			}
			tag := w.Header().Get("ETag")
			if !strings.HasPrefix(tag, `W/"`) {
				t.Fatal("passthrough must use the original file validator")
			}
			r.Header.Set("If-None-Match", tag)
			w = httptest.NewRecorder()
			s.ServeHTTP(w, r)
			if w.Code != http.StatusNotModified || w.Body.Len() != 0 || w.Header().Get("Content-Length") != "" || w.Header().Get("X-IXoSS-Cache") != "BYPASS" {
				t.Fatal("passthrough conditional request lost original semantics")
			}
			if strings.Contains(chain, "auto") && w.Header().Get("Vary") != "Accept" {
				t.Fatal("automatic passthrough lost Vary")
			}
		}
	}
	entry, err := disk.Get(key)
	if err != nil {
		t.Fatal(err)
	}
	defer entry.Close()
	got, err := io.ReadAll(entry)
	if err != nil || !bytes.Equal(got, stale) {
		t.Fatal("passthrough modified the old cache entry")
	}
	count := 0
	if err := filepath.WalkDir(cacheDir, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			count++
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("passthrough wrote %d cache entries, want only the pre-existing one", count)
	}
}

func TestChangedRequestsStillProcessAndCache(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "photo.jpg"), testJPEG(t, 700, 500, 0), 0o600); err != nil {
		t.Fatal(err)
	}
	s := newFullServer(t, root, nil)
	disk, err := cache.NewDisk(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.cache = disk
	for _, chain := range []string{
		"image/resize,w_350", "image/resize,w_700/format,png",
		"image/resize,w_700/quality,q_80", "image/resize,w_700/rotate,0",
		"image/resize,w_700/format,auto",
	} {
		for _, wantCache := range []string{"MISS", "HIT"} {
			r := httptest.NewRequest(http.MethodGet, "/photo.jpg?x-oss-process="+chain, nil)
			r.Header.Set("Accept", "image/webp")
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			if w.Code != http.StatusOK || w.Header().Get("X-IXoSS-Cache") != wantCache || strings.HasPrefix(w.Header().Get("ETag"), "W/") {
				t.Fatalf("%s: status=%d headers=%v", chain, w.Code, w.Header())
			}
			img, _, err := image.Decode(bytes.NewReader(w.Body.Bytes()))
			// image/webp has no stdlib decoder; format detection still confirms it.
			if strings.Contains(chain, "auto") {
				format, err := imagetype.Detect(bytes.NewReader(w.Body.Bytes()), "", "")
				if err != nil || format != imagetype.WEBP {
					t.Fatal("auto must convert JPEG to the negotiated WebP")
				}
			} else if err != nil || img.Bounds().Dx() != wantWidth(chain) {
				t.Fatalf("incorrect transformed output: %v", err)
			}
		}
	}
}

func TestPassthroughResizeUsesMetadataFallback(t *testing.T) {
	root := t.TempDir()
	original := testJPEG(t, 20, 10, 0)
	procServer := newFullServer(t, root, nil)
	for _, format := range []imagetype.Type{imagetype.TIFF, imagetype.AVIF} {
		chain := "image/format," + strings.ToLower(format.String())
		file := "photo." + strings.ToLower(format.String())
		if err := os.WriteFile(filepath.Join(root, "input.jpg"), original, 0o600); err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		procServer.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/input.jpg?x-oss-process="+chain, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("fixture conversion %s: %d %s", format, w.Code, w.Body)
		}
		if err := os.WriteFile(filepath.Join(root, file), w.Body.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}
		s := newLimitedServer(t, root, 1<<20)
		got := httptest.NewRecorder()
		s.ServeHTTP(got, httptest.NewRequest(http.MethodGet, "/"+file+"?x-oss-process=image/resize,w_20", nil))
		if got.Code != http.StatusOK || got.Header().Get("X-IXoSS-Cache") != "BYPASS" || !bytes.Equal(got.Body.Bytes(), w.Body.Bytes()) {
			t.Fatalf("metadata fallback %s: %d %v", format, got.Code, got.Header())
		}
	}
}

func wantWidth(chain string) int {
	if strings.Contains(chain, "w_350") {
		return 350
	}
	return 700
}
