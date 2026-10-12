package httpserver

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vndroid/iXoSS/internal/cache"
)

func TestResponseValidators(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "photo.jpg")
	original := testJPEG(t, 8, 6, 0)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	s := newFullServer(t, root, nil)
	s.cfg.CacheControl = NewDefaultConfig().CacheControl
	disk, err := cache.NewDisk(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.cache = disk
	request := func(method, target, tag, accept string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, target, nil)
		if tag != "" {
			r.Header.Set("If-None-Match", tag)
		}
		r.Header.Set("Accept", accept)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	for _, chain := range []string{"", "image/info", "image/average-hue", "image/resize,w_4/format,png", "image/format,auto"} {
		target := "/photo.jpg"
		if chain != "" {
			target += "?x-oss-process=" + chain
		}
		first := request(http.MethodGet, target, "", "image/webp")
		tag := first.Header().Get("ETag")
		if first.Code != http.StatusOK || tag == "" {
			t.Fatalf("%s: status=%d ETag=%q body=%s", chain, first.Code, tag, first.Body)
		}
		if chain == "" {
			if !strings.HasPrefix(tag, `W/"`) {
				t.Fatal("original must use a weak file validator")
			}
		} else if tag != contentETag(first.Body.Bytes()) {
			t.Fatal("strong ETag must describe the actual response body")
		}
		second := request(http.MethodGet, target, "", "image/webp")
		if second.Header().Get("ETag") != tag || !bytes.Equal(second.Body.Bytes(), first.Body.Bytes()) {
			t.Fatal("cache hit changed the output or ETag")
		}
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			for _, condition := range []string{tag, "W/" + strings.TrimPrefix(tag, "W/"), `"other,tag", ` + tag, "*"} {
				w := request(method, target, condition, "image/webp")
				if w.Code != http.StatusNotModified || w.Body.Len() != 0 || w.Header().Get("Content-Length") != "" {
					t.Fatalf("%s %s If-None-Match=%s: status=%d headers=%v body=%s", method, chain, condition, w.Code, w.Header(), w.Body)
				}
				if w.Header().Get("ETag") != tag || w.Header().Get("Cache-Control") != "public, max-age=300" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
					t.Fatalf("304 lost response policy: %v", w.Header())
				}
				if chain == "image/format,auto" && w.Header().Get("Vary") != "Accept" {
					t.Fatal("304 lost format negotiation Vary")
				}
			}
			w := request(method, target, `"different"`, "image/webp")
			if w.Code != http.StatusOK || w.Header().Get("ETag") != tag {
				t.Fatal("nonmatching validator must return 200")
			}
		}
	}

	auto := "/photo.jpg?x-oss-process=image/format,auto"
	webp := request(http.MethodGet, auto, "", "image/webp")
	fallback := request(http.MethodGet, auto, webp.Header().Get("ETag"), "image/jpeg")
	if fallback.Code != http.StatusOK || fallback.Header().Get("ETag") == webp.Header().Get("ETag") {
		t.Fatal("negotiated variants must not share the content validator")
	}

	before := request(http.MethodGet, "/photo.jpg", "", "").Header().Get("ETag")
	stamp := time.Now().Add(time.Hour)
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if w := request(http.MethodGet, "/photo.jpg", before, ""); w.Code != http.StatusOK || w.Header().Get("ETag") == before {
		t.Fatal("file modification must invalidate the original validator")
	}

	for _, target := range []string{"/healthz", "//photo.jpg", "/missing.jpg", "/photo.jpg?foo=1", "/photo.jpg?x-oss-process=image/resize,w_bad"} {
		w := request(http.MethodGet, target, "*", "")
		if w.Code == http.StatusNotModified || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("%s: incorrect status or policy: %d %v", target, w.Code, w.Header())
		}
	}
	s.verifier = &recordingVerifier{enabled: true, err: errors.New("bad signature")}
	if w := request(http.MethodGet, "/photo.jpg", "*", ""); w.Code != http.StatusForbidden || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("conditional request bypassed authorization")
	}
}

func TestConditionalColdHeadDoesNotEncode(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "photo.jpg"), testJPEG(t, 8, 6, 0), 0o600); err != nil {
		t.Fatal(err)
	}
	s := newLimitedServer(t, root, 1024)
	// This server has no processor: encoding would panic rather than pass.
	for _, tc := range []struct {
		tag    string
		status int
	}{
		{`"old-output"`, http.StatusOK},
		{"*", http.StatusNotModified},
	} {
		r := httptest.NewRequest(http.MethodHead, "/photo.jpg?x-oss-process=image/resize,w_4/format,png", nil)
		r.Header.Set("If-None-Match", tc.tag)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != tc.status || w.Header().Get("ETag") != "" || w.Header().Get("Content-Length") != "" || w.Body.Len() != 0 {
			t.Fatalf("cold HEAD: %d %v %s", w.Code, w.Header(), w.Body)
		}
	}
}

func TestConditionalResponseOverHTTP(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "photo.jpg"), testJPEG(t, 8, 6, 0), 0o600); err != nil {
		t.Fatal(err)
	}
	s := newFullServer(t, root, nil)
	ts := httptest.NewServer(s)
	defer ts.Close()
	res, err := ts.Client().Get(ts.URL + "/photo.jpg?x-oss-process=image/info")
	if err != nil {
		t.Fatal(err)
	}
	tag := res.Header.Get("ETag")
	res.Body.Close()
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		req, err := http.NewRequest(method, ts.URL+"/photo.jpg?x-oss-process=image/info", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Add("If-None-Match", `"unrelated"`)
		req.Header.Add("If-None-Match", tag)
		res, err := ts.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil || res.StatusCode != http.StatusNotModified || len(body) != 0 || res.Header.Get("Content-Length") != "" {
			t.Fatalf("wire 304: status=%d headers=%v body=%s err=%v", res.StatusCode, res.Header, body, err)
		}
	}
}

func TestMalformedValidatorDoesNotMatch(t *testing.T) {
	for _, value := range []string{`abc`, `W/`, `"tag" trailing`, `"tag",`, `"tag", broken`, `"unterminated`, `*, "tag"`, "\"bad space\""} {
		if matchesETag(value, `"tag"`) {
			t.Errorf("malformed validator matched: %s", value)
		}
	}
}
