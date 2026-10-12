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

func TestCanonicalRedirectAfterSigning(t *testing.T) {
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
		r := httptest.NewRequest(http.MethodGet, tc.target+"?x-ixoss-signature=abc", nil)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != http.StatusPermanentRedirect {
			t.Fatalf("%s: status %d, body %s", tc.target, w.Code, w.Body)
		}
		if src.path != "" {
			t.Error("redirect must not open the source")
		}
		if got := w.Header().Get("Location"); got != tc.normalized+"?x-ixoss-signature=abc" {
			t.Errorf("Location = %q", got)
		}
		if v.gotMessage != tc.normalized || v.gotSignature != "abc" {
			t.Errorf("signature checked against %q with %q", v.gotMessage, v.gotSignature)
		}
		if r.URL.Path != tc.raw || r.URL.Query().Get(SignatureQueryKey) != "abc" {
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
			want := http.StatusPermanentRedirect
			if path == "/healthz" {
				want = http.StatusOK
			}
			if w.Code != want {
				t.Errorf("%s %s: status %d", method, path, w.Code)
			}
			if want == http.StatusPermanentRedirect && w.Header().Get("Location") != "/healthz" {
				t.Errorf("incorrect health redirect: %q", w.Header().Get("Location"))
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

func TestCanonicalRedirectPreservesQuery(t *testing.T) {
	s := newLimitedServer(t, t.TempDir(), 1024)
	for _, tc := range []struct{ target, location string }{
		{"//", "/"},
		{"//photo.jpg?x-oss-process=image/info&x-ixoss-signature=a%26b%23c%2Bd%20e", "/photo.jpg?x-oss-process=image/info&x-ixoss-signature=a%26b%23c%2Bd%20e"},
		{"//photo.jpg?x-oss-process=image/resize,w_4/format,png", "/photo.jpg?x-oss-process=image/resize,w_4/format,png"},
		{"//photo.jpg?x%2Doss%2Dprocess=image%2finfo", "/photo.jpg?x%2Doss%2Dprocess=image%2finfo"},
		{"//photo.jpg?x-oss-process=image/info&x-oss-process=image/format,png", "/photo.jpg?x-oss-process=image/info&x-oss-process=image/format,png"},
		{"//evil.example//photo.jpg", "/evil.example/photo.jpg"},
		{"//space%20name%3F%23.jpg", "/space%20name%3F%23.jpg"},
	} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			w := httptest.NewRecorder()
			s.ServeHTTP(w, httptest.NewRequest(method, tc.target, nil))
			if w.Code != http.StatusPermanentRedirect || w.Header().Get("Location") != tc.location {
				t.Errorf("%s %s: status=%d Location=%q, want %q", method, tc.target, w.Code, w.Header().Get("Location"), tc.location)
			}
			if w.Body.Len() != 0 {
				t.Error("redirect unexpectedly returned an image body")
			}
		}
	}
}

func TestInvalidParametersReturnBadRequest(t *testing.T) {
	s := newLimitedServer(t, t.TempDir(), 1024)
	src := &recordingPathSource{Source: s.src}
	s.src = src
	for _, query := range []string{
		"foo=1", "x%2Dunknown=1", "foo", "=1",
		"x-oss-process=image/info&foo=1",
		"x-oss-process=video/info", "x-oss-process=image/unknown",
		"x-oss-process=image/resize,w_bad", "x-oss-process=image/resize,z_1",
		"x-oss-process=style/missing",
	} {
		for _, path := range []string{"/photo.jpg", "//photo.jpg"} {
			for _, method := range []string{http.MethodGet, http.MethodHead} {
				w := httptest.NewRecorder()
				s.ServeHTTP(w, httptest.NewRequest(method, path+"?"+query, nil))
				if w.Code != http.StatusBadRequest || w.Header().Get("Location") != "" {
					t.Errorf("%s %s?%s: status=%d Location=%q", method, path, query, w.Code, w.Header().Get("Location"))
				}
				if method == http.MethodGet && !strings.Contains(w.Body.String(), `"Code":"InvalidArgument"`) {
					t.Errorf("incorrect error body: %s", w.Body)
				}
				if method == http.MethodHead && w.Body.Len() != 0 {
					t.Error("HEAD error returned a body")
				}
				if src.path != "" {
					t.Error("invalid parameters must not open the source")
				}
			}
		}
	}
	for _, path := range []string{"/healthz", "//healthz"} {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path+"?foo=1", nil))
		if w.Code != http.StatusBadRequest || w.Header().Get("Location") != "" {
			t.Errorf("%s: status=%d Location=%q", path, w.Code, w.Header().Get("Location"))
		}
	}
}

func TestInvalidQueryDoesNotRedirect(t *testing.T) {
	s := newLimitedServer(t, t.TempDir(), 1024)
	for _, query := range []string{
		"x-oss-process=%zz", "x-oss-process=%2", "unknown=%zz",
		"x-oss-process=image/info;foo=1", "x-oss-process=image/info\r\nLocation:/evil",
		"x-oss-process=image/info#fragment", "x-oss-process=image info",
		"x-oss-process=图片", "x-oss-process=\"image/info\"",
	} {
		for _, path := range []string{"/healthz", "//photo.jpg"} {
			r := httptest.NewRequest(http.MethodGet, path, nil)
			r.URL.RawQuery = query
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			if w.Code != http.StatusBadRequest || w.Header().Get("Location") != "" {
				t.Errorf("%s?%q: status=%d Location=%q", path, query, w.Code, w.Header().Get("Location"))
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
