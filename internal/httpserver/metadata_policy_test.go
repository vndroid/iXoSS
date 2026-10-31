package httpserver

import (
	"bytes"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/vndroid/iXoSS/internal/cache"
	"github.com/vndroid/iXoSS/internal/imageinfo"
)

func TestIdentityResizeStripsGPSByDefaultAndCacheHitDoesNotOpenSource(t *testing.T) {
	// A tiny valid GPS IFD: latitude reference N, latitude 25 degrees.
	exif := make([]byte, 80)
	copy(exif, []byte{'I', 'I', 42, 0, 8, 0, 0, 0})
	put16 := func(off int, v uint16) { binary.LittleEndian.PutUint16(exif[off:], v) }
	put32 := func(off int, v uint32) { binary.LittleEndian.PutUint32(exif[off:], v) }
	put16(8, 1)
	put16(10, 0x8825)
	put16(12, 4)
	put32(14, 1)
	put32(18, 26)
	put16(26, 2)
	put16(28, 1)
	put16(30, 2)
	put32(32, 2)
	exif[36] = 'N'
	put16(40, 2)
	put16(42, 5)
	put32(44, 3)
	put32(48, 56)
	put32(56, 25)
	put32(60, 1)
	put32(68, 1)
	put32(76, 1)
	payload := append([]byte("Exif\x00\x00"), exif...)
	segment := []byte{0xff, 0xe1, byte((len(payload) + 2) >> 8), byte(len(payload) + 2)}
	original := testJPEG(t, 8, 6, 0)
	original = append(append(append([]byte{}, original[:2]...), append(segment, payload...)...), original[2:]...)
	info, err := imageinfo.Read(bytes.NewReader(original), int64(len(original)))
	if err != nil || info.Exif["GPSLatitude"] == "" {
		t.Fatalf("GPS fixture invalid: %+v %v", info, err)
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "photo.jpg"), original, 0600); err != nil {
		t.Fatal(err)
	}
	s := newFullServer(t, root, nil)
	s.cache, err = cache.NewDisk(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	src := &recordingPathSource{Source: s.src}
	s.src = src
	url := "/photo.jpg?x-oss-process=image/resize,w_8/format,jpg"
	for _, status := range []string{"MISS", "HIT"} {
		src.path = ""
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, url, nil))
		if w.Code != 200 || w.Header().Get("X-IXoSS-Cache") != status {
			t.Fatalf("%s: %d %s", status, w.Code, w.Body)
		}
		result, err := imageinfo.Read(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
		if err != nil || result.Exif["GPSLatitude"] != "" {
			t.Fatalf("GPS was not stripped: %+v %v", result, err)
		}
		if status == "HIT" && src.path != "" {
			t.Fatal("cache hit opened the source")
		}
	}
}
