package vips

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"image"
	"image/png"
	"math"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/vndroid/iXoSS/internal/imagedata"
	"github.com/vndroid/iXoSS/internal/imagetype"
)

// A two-color RLE8 image with an absolute run. Odd runs include word padding,
// even when only part of the declared run fits the image's width.
func bmpRLE8(width, run int) []byte {
	pixels := append([]byte{0, byte(run)}, bytes.Repeat([]byte{1}, run)...)
	if run%2 != 0 {
		pixels = append(pixels, 0)
	}
	pixels = append(pixels, 0, 1) // RLE end of bitmap.
	data := make([]byte, 62+len(pixels))
	copy(data, "BM")
	binary.LittleEndian.PutUint32(data[2:6], uint32(len(data)))
	binary.LittleEndian.PutUint32(data[10:14], 62)
	binary.LittleEndian.PutUint32(data[14:18], 40)
	binary.LittleEndian.PutUint32(data[18:22], uint32(width))
	binary.LittleEndian.PutUint32(data[22:26], 1)
	binary.LittleEndian.PutUint16(data[26:28], 1)
	binary.LittleEndian.PutUint16(data[28:30], 8)
	binary.LittleEndian.PutUint32(data[30:34], 1) // BI_RLE8.
	binary.LittleEndian.PutUint32(data[34:38], uint32(len(pixels)))
	binary.LittleEndian.PutUint32(data[46:50], 2)
	data[60] = 255 // Palette entry 1: red (BGRA).
	copy(data[62:], pixels)
	return data
}

func TestBMPRLE8AbsoluteRuns(t *testing.T) {
	for _, sample := range [][2]int{{1, 255}, {62, 255}, {63, 255}, {1, 254}, {255, 255}, {3, 3}} {
		width, run := sample[0], sample[1]
		t.Run(strconv.Itoa(width)+"/"+strconv.Itoa(run), func(t *testing.T) {
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			defer Cleanup()
			data := imagedata.NewFromBytesWithFormat(imagetype.BMP, bmpRLE8(width, run))
			defer data.Close()
			img := new(Image)
			defer img.Clear()
			if err := img.Load(data, 1, 0, 1); err != nil {
				t.Fatal(err)
			}
			// Load is lazy. Force the native RLE decoder before checking pixels.
			if err := img.CopyMemory(); err != nil {
				t.Fatal(err)
			}
			out, err := img.Save(imagetype.PNG, 80, SaveOverrides{})
			if err != nil {
				t.Fatal(err)
			}
			defer out.Close()
			decoded, err := png.Decode(out.Reader())
			if err != nil {
				t.Fatal(err)
			}
			if decoded.Bounds().Dx() != width || decoded.Bounds().Dy() != 1 {
				t.Fatalf("unexpected dimensions: %v", decoded.Bounds())
			}
			for x := 0; x < width; x++ {
				r, g, b, a := decoded.At(x, 0).RGBA()
				if r != 65535 || g != 0 || b != 0 || a != 65535 {
					t.Fatalf("pixel %d: %d %d %d %d", x, r, g, b, a)
				}
			}
		})
	}
}

func TestBMPRLE8TruncatedAbsoluteRun(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer Cleanup()
	fixture := bmpRLE8(1, 255)
	fixture = fixture[:62+2+254] // Truncate inside the declared 255-byte run.
	data := imagedata.NewFromBytesWithFormat(imagetype.BMP, fixture)
	defer data.Close()
	img := new(Image)
	defer img.Clear()
	if err := img.Load(data, 1, 0, 1); err != nil {
		return
	}
	if err := img.CopyMemory(); err == nil {
		t.Fatal("truncated RLE8 data decoded successfully")
	}
}

// An uncompressed BMP whose header declares the given geometry, with filler
// pixel bytes: the fixtures from imgproxy a0bbb32, built here instead.
func bmpDeclared(width, height int32, bpp uint16, colors uint32, pixels int) []byte {
	offset := 54 + 4*int(colors)
	data := make([]byte, offset+pixels)
	copy(data, "BM")
	binary.LittleEndian.PutUint32(data[2:6], uint32(len(data)))
	binary.LittleEndian.PutUint32(data[10:14], uint32(offset))
	binary.LittleEndian.PutUint32(data[14:18], 40)
	binary.LittleEndian.PutUint32(data[18:22], uint32(width))
	binary.LittleEndian.PutUint32(data[22:26], uint32(height))
	binary.LittleEndian.PutUint16(data[26:28], 1)
	binary.LittleEndian.PutUint16(data[28:30], bpp)
	binary.LittleEndian.PutUint32(data[46:50], colors)
	for i := 54; i < len(data); i++ {
		data[i] = 0xAA
	}
	return data
}

// Dimensions that overflowed the row-size arithmetic (imgproxy
// GHSA-c4f2-mxv4-6fp5, v4.0.21) must be rejected before any row is decoded.
func TestBMPRejectsOverflowingDimensions(t *testing.T) {
	for name, fixture := range map[string][]byte{
		"width-overflow-8bpp":  bmpDeclared(536870911, 1, 8, 256, 4096),
		"width-int-max":        bmpDeclared(math.MaxInt32, 1, 24, 0, 4096),
		"width-too-large":      bmpDeclared(10000001, 1, 24, 0, 4096),
		"height-int-min":       bmpDeclared(1, math.MinInt32, 24, 0, 4096),
		"area-too-large":       bmpDeclared(100000, 100000, 24, 0, 4096),
		"width-overflow-32bpp": bmpDeclared(0x20000000, 1, 32, 0, 4096),
		// Bottom-up height above vips_sequential's tile_height maximum.
		"bottom-up-too-tall": bmpDeclared(1, 1000001, 24, 0, 0),
	} {
		t.Run(name, func(t *testing.T) {
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			defer Cleanup()
			data := imagedata.NewFromBytesWithFormat(imagetype.BMP, fixture)
			defer data.Close()
			img := new(Image)
			defer img.Clear()
			err := img.Load(data, 1, 0, 1)
			if err == nil {
				// Load is lazy; a header that slipped through must still fail here.
				err = img.CopyMemory()
			}
			if err == nil || !strings.Contains(err.Error(), "unsupported BMP image dimensions") {
				t.Fatalf("got %v, want unsupported BMP image dimensions", err)
			}
		})
	}
}

// bpp=40 at width 512 once read a 2560-byte row into a 2052-byte buffer. The
// bpp check (imgproxy 60d367c) predates this port's baseline; this pins it.
func TestBMPRejectsUnsupportedBitDepth(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer Cleanup()
	data := imagedata.NewFromBytesWithFormat(imagetype.BMP, bmpDeclared(512, 1, 40, 0, 2560))
	defer data.Close()
	img := new(Image)
	defer img.Clear()
	err := img.Load(data, 1, 0, 1)
	if err == nil {
		err = img.CopyMemory()
	}
	if err == nil || !strings.Contains(err.Error(), "unsupported bit depth") {
		t.Fatalf("got %v, want unsupported bit depth", err)
	}
}

// Samples from imgproxy 823aa6b, byte for byte.
const (
	bmp555BIRGB = "" +
		"424d3e0000000000000036000000280000000200000002000000010010000000" +
		"00000800000000000000000000000000000000000000ff7fff7fff7fff7f"
	bmp555Bitfields = "" +
		"424d8e000000000000008a0000007c0000000200000001000000010010000300" +
		"00000400000000000000000000000000000000000000007c0000e00300001f00" +
		"0000000000004247527300000000000000000000000000000000000000000000" +
		"0000000000000000000000000000000000000000000000000000040000000000" +
		"00000000000000000000ff7fff7f"
	bmpRLE8MoveTo = "" +
		"424d5c0000000000000042000000280000000200000004000000010008000100" +
		"00000e0000000000000000000000030000000000000020202000383838005050" +
		"50000201000000020001020200000001"
	bmpTruncatedPalette = "" +
		"424d7a0000000000000076000000280000000200000001000000010008000000" +
		"000004000000000000000000000010000000000000000a0a0a000a0a0a000a0a" +
		"0a000a0a0a000a0a0a000a0a0a000a0a0a000a0a0a000a0a0a000a0a0a000a0a" +
		"0a000a0a0a000a0a0a000a0a0a000a0a0a000a0a0a00c8c80000"
)

func decodeBMPSample(t *testing.T, sample string) image.Image {
	t.Helper()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer Cleanup()
	raw, err := hex.DecodeString(sample)
	if err != nil {
		t.Fatal(err)
	}
	data := imagedata.NewFromBytesWithFormat(imagetype.BMP, raw)
	defer data.Close()
	img := new(Image)
	defer img.Clear()
	if err := img.Load(data, 1, 0, 1); err != nil {
		t.Fatal(err)
	}
	out, err := img.Save(imagetype.PNG, 80, SaveOverrides{})
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	decoded, err := png.Decode(out.Reader())
	if err != nil {
		t.Fatal(err)
	}
	return decoded
}

// 16-bit BMPs are RGB555 under BI_RGB, and BITFIELDS may declare 5-5-5. Every
// source pixel is 0x7FFF, white in RGB555; these used to decode as black and
// as a tinted grey.
func TestBMP16BitRGB555(t *testing.T) {
	for name, sample := range map[string]string{"bi_rgb": bmp555BIRGB, "bitfields": bmp555Bitfields} {
		t.Run(name, func(t *testing.T) {
			decoded := decodeBMPSample(t, sample)
			for y := decoded.Bounds().Min.Y; y < decoded.Bounds().Max.Y; y++ {
				for x := decoded.Bounds().Min.X; x < decoded.Bounds().Max.X; x++ {
					r, g, b, _ := decoded.At(x, y).RGBA()
					if r>>8 < 248 || g>>8 < 248 || b>>8 < 248 {
						t.Fatalf("pixel %d,%d: %02x%02x%02x, want near white", x, y, r>>8, g>>8, b>>8)
					}
				}
			}
		})
	}
}

// RLE8 MOVE_TO must move exactly dy rows: the stream writes rows 0 and 2 of a
// four-row bottom-up image, which are image rows 3 and 1.
func TestBMPRLE8MoveToDy(t *testing.T) {
	decoded := decodeBMPSample(t, bmpRLE8MoveTo)
	for y, want := range []uint32{0x00, 0x50, 0x00, 0x38} {
		if r, _, _, _ := decoded.At(0, y).RGBA(); r>>8 != want {
			t.Errorf("row %d: red %02x, want %02x", y, r>>8, want)
		}
	}
}

// Indices past a truncated palette read zeroed memory, never heap contents.
func TestBMPTruncatedPaletteReadsZeros(t *testing.T) {
	decoded := decodeBMPSample(t, bmpTruncatedPalette)
	if r, g, b, _ := decoded.At(0, 0).RGBA(); r != 0 || g != 0 || b != 0 {
		t.Fatalf("got %02x%02x%02x, want black", r>>8, g>>8, b>>8)
	}
}
