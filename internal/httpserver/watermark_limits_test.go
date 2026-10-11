package httpserver

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vndroid/iXoSS/internal/imagedata"
	"github.com/vndroid/iXoSS/internal/source"
)

func solidPNG(t *testing.T, side int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, side, side))
	for y := range side {
		for x := range side {
			img.Set(x, y, color.RGBA{10, 20, 30, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func noisePNG(t *testing.T, approxBytes int) []byte {
	t.Helper()
	side := 1
	for side*side*3 < approxBytes {
		side++
	}
	img := image.NewRGBA(image.Rect(0, 0, side, side))
	seed := uint32(1)
	for y := range side {
		for x := range side {
			seed = seed*1664525 + 1013904223
			img.Set(x, y, color.RGBA{uint8(seed >> 16), uint8(seed >> 8), uint8(seed), 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func watermarkDir(t *testing.T, files map[string][]byte) source.Source {
	t.Helper()
	dir := t.TempDir()
	for name, b := range files {
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	src, err := source.NewLocal(dir)
	if err != nil {
		t.Fatal(err)
	}
	return src
}

func heldBytes(p *watermarkProvider) int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.usedBytes
}

func TestWatermarkCacheIsBounded(t *testing.T) {
	p := newTestProvider(t)
	defer p.Close()
	blob := solidPNG(t, 4)
	for i := range maxWatermarks * 4 {
		d, err := imagedata.NewFromBytes(blob)
		if err != nil {
			t.Fatal(err)
		}
		p.store(strconv.Itoa(i), d)
	}
	if got := p.size(); got != maxWatermarks {
		t.Errorf("cache holds %d entries, want %d", got, maxWatermarks)
	}
}

func TestWatermarkReferenceSurvivesEviction(t *testing.T) {
	p := newTestProvider(t)
	defer p.Close()
	held, _, err := p.Get(context.Background(), imageOptions("wm.png"))
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	blob := solidPNG(t, 4)
	for i := range maxWatermarks {
		d, err := imagedata.NewFromBytes(blob)
		if err != nil {
			t.Fatal(err)
		}
		p.store(strconv.Itoa(i), d)
	}
	if d := p.hit(watermarkSpec{path: "wm.png"}.cacheKey(), time.Now()); d != nil {
		d.Close()
		t.Fatal("original entry was not evicted")
	}
	if _, err := held.Size(); err != nil {
		t.Fatalf("eviction freed a caller's held reference: %v", err)
	}
}

type countingLocalSource struct {
	source.Source
	opens atomic.Int32
}

func (s *countingLocalSource) Open(ctx context.Context, path string) (*source.Object, error) {
	s.opens.Add(1)
	time.Sleep(30 * time.Millisecond)
	return s.Source.Open(ctx, path)
}

func TestWatermarkConcurrentMissReadsOnce(t *testing.T) {
	src := &countingLocalSource{Source: watermarkDir(t, map[string][]byte{
		"wm.png": noisePNG(t, 256<<10),
	})}
	p := newWatermarkProvider(src, NewDefaultConfig())
	defer p.Close()
	const n = 16
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range n {
		wg.Go(func() {
			<-start
			d, _, err := p.Get(context.Background(), imageOptions("wm.png"))
			if err != nil || d == nil {
				t.Errorf("concurrent watermark read failed: %v", err)
				return
			}
			if _, err := d.Size(); err != nil {
				t.Errorf("watermark reference is invalid: %v", err)
			}
			d.Close()
		})
	}
	close(start)
	wg.Wait()
	if got := src.opens.Load(); got != 1 {
		t.Errorf("%d requests caused %d file reads, want 1", n, got)
	}
}

func TestWatermarkResolutionIsBounded(t *testing.T) {
	small := solidPNG(t, 1000) // 1 MP
	big := solidPNG(t, 4000)   // 16 MP
	t.Logf("1 MP is %d bytes on the wire; 16 MP is %d", len(small), len(big))

	src := watermarkDir(t, map[string][]byte{"small.png": small, "big.png": big})

	cfg := NewDefaultConfig()
	cfg.MaxWatermarkResolution = 4_000_000 // 4 MP
	p := newWatermarkProvider(src, cfg)
	defer p.Close()

	d, _, err := p.Get(context.Background(), imageOptions("small.png"))
	if err != nil {
		t.Fatalf("1 MP watermark refused: %v", err)
	}
	d.Close()

	_, _, err = p.Get(context.Background(), imageOptions("big.png"))
	if !errors.Is(err, ErrWatermarkTooLarge) {
		t.Errorf("16 MP watermark: err = %v, want ErrWatermarkTooLarge", err)
	}
	if p.size() != 1 {
		t.Errorf("cache holds %d entries, want only the accepted one", p.size())
	}

	// The wire size is no defence here: the refused image is smaller than the
	// accepted one would be at any sane byte limit.
	if len(big) > 2<<20 {
		t.Errorf("fixture is %d bytes, too big to make the point", len(big))
	}
}

func TestWatermarkCacheBytesAreBounded(t *testing.T) {
	const each = 1 << 20
	blob := noisePNG(t, each)

	files := map[string][]byte{}
	names := make([]string, 32)
	for i := range names {
		names[i] = "wm" + strconv.Itoa(i) + ".png"
		files[names[i]] = blob
	}
	src := watermarkDir(t, files)

	cfg := NewDefaultConfig()
	cfg.WatermarkCacheBytes = 8 << 20 // room for ~8 of them
	p := newWatermarkProvider(src, cfg)
	defer p.Close()

	for _, name := range names {
		d, _, err := p.Get(context.Background(), imageOptions(name))
		if err != nil {
			t.Fatal(err)
		}
		d.Close()
	}

	held := heldBytes(p)
	t.Logf("%d x %d-byte watermarks -> %d entries, %d bytes held (budget %d)",
		len(names), len(blob), p.size(), held, cfg.WatermarkCacheBytes)

	if held > cfg.WatermarkCacheBytes {
		t.Errorf("held %d bytes, budget is %d", held, cfg.WatermarkCacheBytes)
	}
	if p.size() == 0 {
		t.Error("the cache evicted everything; the budget should hold several")
	}
}

func TestWatermarkByteAccountingStaysExact(t *testing.T) {
	blob := noisePNG(t, 256<<10)
	files := map[string][]byte{}
	names := make([]string, 20)
	for i := range names {
		names[i] = "wm" + strconv.Itoa(i) + ".png"
		files[names[i]] = blob
	}
	src := watermarkDir(t, files)

	cfg := NewDefaultConfig()
	cfg.WatermarkCacheBytes = 2 << 20
	p := newWatermarkProvider(src, cfg)
	defer p.Close()

	check := func(stage string) {
		p.mu.Lock()
		var want int64
		for _, c := range p.loaded {
			want += c.size
		}
		got := p.usedBytes
		p.mu.Unlock()
		if got != want {
			t.Errorf("%s: usedBytes = %d, entries add up to %d", stage, got, want)
		}
	}

	for _, name := range names {
		d, _, err := p.Get(context.Background(), imageOptions(name))
		if err != nil {
			t.Fatal(err)
		}
		d.Close()
	}
	check("after LRU eviction")

	d, _, err := p.Get(context.Background(), imageOptions(names[0]))
	if err != nil {
		t.Fatal(err)
	}
	d.Close()
	check("after another eviction")

	d, _, err = p.Get(context.Background(), imageOptions(names[0]))
	if err != nil {
		t.Fatal(err)
	}
	d.Close()
	check("after a hit")

	p.Close()
	if got := heldBytes(p); got != 0 {
		t.Errorf("after Close, usedBytes = %d, want 0", got)
	}
}

func TestWatermarkPixelCountDoesNotOverflow(t *testing.T) {
	tests := []struct {
		name         string
		w, h, frames int
		wantOK       bool
		want         int64
	}{
		{"ordinary", 4000, 4000, 1, true, 16_000_000},
		{"animated", 1000, 1000, 60, true, 60_000_000},
		{"large but representable", 100000, 100000, 1, true, 10_000_000_000},
		{"uint32 squared overflows int64", 4294967295, 4294967295, 1, false, 0},
		{"frames push it over", 3037000500, 3037000500, 2, false, 0},
		{"zero width", 0, 100, 1, false, 0},
		{"negative height", 100, -1, 1, false, 0},
		{"zero frames", 100, 100, 0, false, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := pixelCount(tt.w, tt.h, tt.frames)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v (got %d)", ok, tt.wantOK, got)
			}
			if ok && got != tt.want {
				t.Errorf("= %d, want %d", got, tt.want)
			}
		})
	}
}

func TestWatermarkHeaderOverflowIsRefused(t *testing.T) {
	p := newTestProvider(t)
	defer p.Close()
	p.maxResolution = 8_000_000

	pngHeader := func(w, h uint32) []byte {
		b := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}
		ihdr := make([]byte, 25)
		binary.BigEndian.PutUint32(ihdr[0:], 13)
		copy(ihdr[4:], "IHDR")
		binary.BigEndian.PutUint32(ihdr[8:], w)
		binary.BigEndian.PutUint32(ihdr[12:], h)
		ihdr[16] = 8
		ihdr[17] = 6
		return append(b, ihdr...)
	}

	for _, c := range []struct {
		name string
		w, h uint32
	}{
		{"honest oversize", 4000, 4000},
		{"crafted to overflow", 4294967295, 4294967295},
		{"crafted, one dimension maximal", 4294967295, 2147483648},
	} {
		t.Run(c.name, func(t *testing.T) {
			if err := p.checkResolution(pngHeader(c.w, c.h)); err == nil {
				t.Errorf("%dx%d passed an 8 MP limit", c.w, c.h)
			}
		})
	}

	// And an honest small one still passes.
	if err := p.checkResolution(pngHeader(100, 100)); err != nil {
		t.Errorf("100x100 refused: %v", err)
	}
}

func TestWatermarkTextEstimateDoesNotOverRefuse(t *testing.T) {
	p := newTestProvider(t)
	defer p.Close()
	p.maxResolution = 8_000_000

	ok := []struct {
		text string
		size int
	}{
		{"hello", 40},
		{"© Example Corp 2026", 40},
		{strings.Repeat("i", 200), 40},
		{"图片水印测试", 100},
		{strings.Repeat("W", 20), 200},
		{"one\ntwo\nthree", 100},
		{strings.Repeat("W", 20), 1000}, // renders at 14 MP; the estimate must not be the thing that stops it
	}
	for _, c := range ok {
		if err := p.checkTextEstimate(watermarkSpec{text: c.text, size: c.size}); err != nil {
			t.Errorf("estimate refused %d runes at size %d: %v", len([]rune(c.text)), c.size, err)
		}
	}

	// And it must refuse what cannot possibly fit.
	tooBig := []struct {
		text string
		size int
	}{
		{strings.Repeat("W", 8000), 1000},
		{strings.Repeat("W", 400), 1000},
		{strings.Repeat("W\n", 5000), 1000},
	}
	for _, c := range tooBig {
		if err := p.checkTextEstimate(watermarkSpec{text: c.text, size: c.size}); !errors.Is(err, ErrWatermarkTooLarge) {
			t.Errorf("estimate accepted %d runes at size %d: %v", len([]rune(c.text)), c.size, err)
		}
	}
}
