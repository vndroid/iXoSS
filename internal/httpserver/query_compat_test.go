package httpserver

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestUnknownQueryParametersAreIgnored(t *testing.T) {
	root := t.TempDir()
	data := testJPEG(t, 8, 6, 0)
	if err := os.WriteFile(filepath.Join(root, "photo.jpg"), data, 0600); err != nil {
		t.Fatal(err)
	}
	s := newLimitedServer(t, root, 1024)
	verifier := &recordingVerifier{enabled: true}
	s.verifier = verifier
	for _, query := range []string{"v=2&utm_source=x", "foo&=1", "v=3&x-ixoss-signature=abc"} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			w := httptest.NewRecorder()
			s.ServeHTTP(w, httptest.NewRequest(method, "/photo.jpg?"+query, nil))
			if w.Code != 200 || w.Header().Get("Location") != "" {
				t.Fatalf("ignored query: %d %v", w.Code, w.Header())
			}
			if method == http.MethodGet && !bytes.Equal(w.Body.Bytes(), data) {
				t.Fatal("original changed")
			}
		}
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "//photo.jpg?v=2&utm_source=x", nil))
	if w.Code != 308 || w.Header().Get("Location") != "/photo.jpg?v=2&utm_source=x" {
		t.Fatal("path redirect lost ignored parameters")
	}
}
