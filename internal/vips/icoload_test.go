package vips

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"math"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/vndroid/iXoSS/internal/imagedata"
	"github.com/vndroid/iXoSS/internal/imagetype"
)

func icoEntry(payload []byte, declaredSize uint32) []byte {
	data := make([]byte, 22+len(payload))
	binary.LittleEndian.PutUint16(data[2:4], 1)
	binary.LittleEndian.PutUint16(data[4:6], 1)
	data[6], data[7] = 1, 1
	binary.LittleEndian.PutUint16(data[10:12], 1)
	binary.LittleEndian.PutUint16(data[12:14], 32)
	binary.LittleEndian.PutUint32(data[14:18], declaredSize)
	binary.LittleEndian.PutUint32(data[18:22], 22)
	copy(data[22:], payload)
	return data
}

func TestIcoRejectsUnsafeEntrySizes(t *testing.T) {
	// 26..39 bytes also catch the incomplete v4.0.16 fix: prepending the
	// 14-byte BMP file header does not make the payload's 40-byte header valid.
	for _, size := range []uint32{1, 8, 25, 26, 32, 35, 39, 10*1024*1024 + 1, math.MaxUint32} {
		t.Run(strconv.FormatUint(uint64(size), 10), func(t *testing.T) {
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			defer Cleanup()
			payload := make([]byte, min(size, 39))
			data := imagedata.NewFromBytesWithFormat(imagetype.ICO, icoEntry(payload, size))
			defer data.Close()
			img := new(Image)
			defer img.Clear()
			err := img.Load(data, 1, 0, 1)
			want := "ICO image data is too small"
			if size > 10*1024*1024 {
				want = "ICO image data is too large"
			}
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("declared size %d: got %v, want %q", size, err, want)
			}
		})
	}
}

func TestIcoValidPNGAndBMPPayloads(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	src.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, src); err != nil {
		t.Fatal(err)
	}
	bmp := make([]byte, 48) // BITMAPINFOHEADER, BGRA pixel, padded AND mask.
	binary.LittleEndian.PutUint32(bmp[0:4], 40)
	binary.LittleEndian.PutUint32(bmp[4:8], 1)
	binary.LittleEndian.PutUint32(bmp[8:12], 2) // Includes the AND mask height.
	binary.LittleEndian.PutUint16(bmp[12:14], 1)
	binary.LittleEndian.PutUint16(bmp[14:16], 32)
	bmp[42], bmp[43] = 255, 255
	for name, payload := range map[string][]byte{"png": encoded.Bytes(), "bmp": bmp} {
		t.Run(name, func(t *testing.T) {
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			defer Cleanup()
			data := imagedata.NewFromBytesWithFormat(imagetype.ICO, icoEntry(payload, uint32(len(payload))))
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
			if decoded.Bounds().Dx() != 1 || decoded.Bounds().Dy() != 1 {
				t.Fatalf("unexpected dimensions: %v", decoded.Bounds())
			}
			r, g, b, a := decoded.At(0, 0).RGBA()
			if r != 65535 || g != 0 || b != 0 || a != 65535 {
				t.Fatalf("unexpected pixel: %d %d %d %d", r, g, b, a)
			}
		})
	}
}
