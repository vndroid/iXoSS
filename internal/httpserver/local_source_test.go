package httpserver

import (
	"bytes"
	"context"
	"image"
	_ "image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vndroid/iXoSS/internal/auximageprovider"
	"github.com/vndroid/iXoSS/internal/processing"
	"github.com/vndroid/iXoSS/internal/security"
	"github.com/vndroid/iXoSS/internal/source"
)

type recordingPathSource struct {
	source.Source
	path string
}

func (s *recordingPathSource) Open(ctx context.Context, path string) (*source.Object, error) {
	s.path = path
	return s.Source.Open(ctx, path)
}

func TestNormalizeRequestPathAfterSigning(t *testing.T) {
	s := newLimitedServer(t, t.TempDir(), 1024)
	v := &recordingVerifier{enabled: true}
	s.verifier = v
	src := &recordingPathSource{Source: s.src}
	s.src = src
	for _, tc := range []struct{ target, raw, normalized string }{
		{"//", "//", "/"},
		{"////", "////", "/"},
		{"//sub///photo.jpg", "//sub///photo.jpg", "/sub/photo.jpg"},
		{"/sub%2F%2Fphoto.jpg", "/sub//photo.jpg", "/sub/photo.jpg"},
		{"/sub//./../photo.jpg", "/sub//./../photo.jpg", "/sub/./../photo.jpg"},
		{"/missing///", "/missing///", "/missing/"},
	} {
		r := httptest.NewRequest(http.MethodGet, tc.target+"?v=1&x-ixoss-signature=abc", nil)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != http.StatusNotFound {
			t.Fatalf("%s: status %d, body %s", tc.target, w.Code, w.Body)
		}
		if src.path != tc.normalized {
			t.Errorf("source path = %q, want %q", src.path, tc.normalized)
		}
		if v.gotMessage != tc.raw || v.gotSignature != "abc" {
			t.Errorf("signature checked against %q with %q", v.gotMessage, v.gotSignature)
		}
		if r.URL.Path != tc.raw || r.URL.Query().Get("v") != "1" {
			t.Error("original request was modified")
		}
	}
}

func TestNormalizedHealthPath(t *testing.T) {
	s := newLimitedServer(t, t.TempDir(), 1024)
	v := &recordingVerifier{enabled: true}
	s.verifier = v
	for _, path := range []string{"/healthz", "//healthz", "////healthz", "/%2Fhealthz"} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			w := httptest.NewRecorder()
			s.ServeHTTP(w, httptest.NewRequest(method, path, nil))
			if w.Code != http.StatusOK {
				t.Errorf("%s %s: status %d", method, path, w.Code)
			}
		}
	}
	if v.calls != 0 {
		t.Error("normalized health path must remain exempt from signatures")
	}
}

func TestRepeatedSlashesOverHTTP(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	original := testJPEG(t, 8, 6, 0)
	if err := os.WriteFile(filepath.Join(root, "sub", "photo.jpg"), original, 0o600); err != nil {
		t.Fatal(err)
	}
	s := newFullServer(t, root, nil)
	cfg := security.NewDefaultConfig()
	cfg.Keys = [][]byte{[]byte("test key")}
	cfg.Salts = [][]byte{[]byte("test salt")}
	checker, err := security.New(&cfg)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s)
	defer ts.Close()
	client := ts.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	for _, signed := range []bool{false, true} {
		if signed {
			s.verifier = checker
		}
		for _, path := range []string{"/sub/photo.jpg", "//sub//photo.jpg", "/sub//photo.jpg", "///sub///photo.jpg", "/sub%2F%2Fphoto.jpg"} {
			for _, chain := range []string{"", "image/info", "image/resize,w_4/format,png"} {
				for _, method := range []string{http.MethodGet, http.MethodHead} {
					target := ts.URL + path
					if chain != "" {
						target += "?x-oss-process=" + chain
					}
					req, err := http.NewRequest(method, target, nil)
					if err != nil {
						t.Fatal(err)
					}
					if signed {
						sig, err := checker.Sign(SignedMessage(req.URL.Path, chain))
						if err != nil {
							t.Fatal(err)
						}
						q := req.URL.Query()
						q.Set(SignatureQueryKey, sig)
						req.URL.RawQuery = q.Encode()
					}
					res, err := client.Do(req)
					if err != nil {
						t.Fatal(err)
					}
					body, err := io.ReadAll(res.Body)
					res.Body.Close()
					if err != nil {
						t.Fatal(err)
					}
					if res.StatusCode != http.StatusOK {
						t.Fatalf("signed=%t %s %s: status %d, body %s", signed, method, target, res.StatusCode, body)
					}
					switch {
					case method == http.MethodHead:
						if len(body) != 0 {
							t.Fatal("HEAD returned a body")
						}
					case chain == "":
						if !bytes.Equal(body, original) {
							t.Fatal("original image changed")
						}
					case chain == "image/info":
						if !strings.Contains(string(body), `"ImageWidth":{"value":"8"}`) {
							t.Fatalf("incorrect image info: %s", body)
						}
					default:
						img, format, err := image.Decode(bytes.NewReader(body))
						if err != nil || format != "png" {
							t.Fatalf("invalid PNG response: format=%s err=%v", format, err)
						}
						if img.Bounds().Dx() != 4 || img.Bounds().Dy() != 3 {
							t.Fatalf("incorrect resized dimensions: %v", img.Bounds())
						}
					}
				}
			}
		}
	}
}

func TestNewRequiresRoot(t *testing.T) {
	called := false
	_, err := New(Config{}, func(auximageprovider.Provider) (*processing.Processor, error) {
		called = true
		return nil, nil
	})
	if err == nil {
		t.Fatal("New without Root must fail rather than serve the working directory")
	}
	if called {
		t.Error("processor initialized before rejecting the missing root")
	}
}

func TestLocalCacheKeyIsUnchanged(t *testing.T) {
	s := newLimitedServer(t, t.TempDir(), 1024)
	info := &source.Info{Key: "/images/x.jpg", Size: 10, ModTime: 1700000000}
	got := s.cacheKey(info, "image/format,png")
	// Pin the digest produced by the previous local implementation, so removing
	// remote validators cannot silently invalidate an existing disk cache.
	const legacyHash = "4e4bdc167588b635704a41b84fe2df8b3c5b94b5fb033333058571da7e943f4f"
	if got.Hash() != legacyHash {
		t.Fatalf("local cache hash = %s, want %s", got.Hash(), legacyHash)
	}
	info.ModTime++
	if s.cacheKey(info, got.Chain).Hash() == got.Hash() {
		t.Error("a changed modification time must invalidate a cached transform")
	}
}
