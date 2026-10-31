package cache

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"sync"
	"testing"
)

func TestStoredDigestAndLogicalReader(t *testing.T) {
	d, err := NewDisk(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	k := Key{SourcePath: "/image"}
	data := []byte("encoded image bytes")
	if err := d.Put(k, data); err != nil {
		t.Fatal(err)
	}
	e, err := d.Get(k)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	want := fmt.Sprintf(`"%x"`, sha256.Sum256(data))
	if e.ETag != want || e.Size != int64(len(data)) {
		t.Fatalf("invalid entry: %+v", e)
	}
	b, err := io.ReadAll(e)
	if err != nil || !bytes.Equal(b, data) {
		t.Fatalf("read: %q %v", b, err)
	}
	if _, err := e.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if _, err := e.WriteTo(&out); err != nil || !bytes.Equal(out.Bytes(), data) {
		t.Fatal("WriteTo leaked header")
	}
	// Old raw files are misses: never hash whole legacy files on the hot path.
	if err := os.WriteFile(d.path(k), data, 0600); err != nil {
		t.Fatal(err)
	}
	if e, err := d.Get(k); err != ErrMiss {
		if e != nil {
			e.Close()
		}
		t.Fatalf("legacy entry: %v", err)
	}
}

func TestConcurrentCacheReplacementKeepsDigestWithData(t *testing.T) {
	d, _ := NewDisk(t.TempDir())
	k := Key{SourcePath: "/image"}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			data := bytes.Repeat([]byte{byte(i)}, 1024)
			for j := 0; j < 10; j++ {
				if err := d.Put(k, data); err != nil {
					t.Error(err)
					return
				}
				e, err := d.Get(k)
				if err != nil {
					t.Error(err)
					return
				}
				b, err := io.ReadAll(e)
				e.Close()
				if err != nil || e.ETag != fmt.Sprintf(`"%x"`, sha256.Sum256(b)) {
					t.Error("digest and data belong to different writes")
					return
				}
			}
		}(i)
	}
	wg.Wait()
}
