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

// A BMP ICO payload declaring the given header length, geometry and palette,
// as in the imgproxy a0bbb32 fixtures.
func icoBMPPayload(headerLen uint32, width int32, bpp uint16, colors uint32) []byte {
	payload := make([]byte, 4136)
	binary.LittleEndian.PutUint32(payload[0:4], headerLen)
	binary.LittleEndian.PutUint32(payload[4:8], uint32(width))
	binary.LittleEndian.PutUint32(payload[8:12], 2) // Includes the AND mask height.
	binary.LittleEndian.PutUint16(payload[12:14], 1)
	binary.LittleEndian.PutUint16(payload[14:16], bpp)
	binary.LittleEndian.PutUint32(payload[32:36], colors)
	for i := 40; i < len(payload); i++ {
		payload[i] = 0xAA
	}
	return payload
}

// The synthesized BMP header must not trust the payload's header length,
// width or palette size (imgproxy v4.0.21).
func TestIcoRejectsUnsafeBMPHeaders(t *testing.T) {
	for name, tc := range map[string]struct {
		payload []byte
		want    string
	}{
		"bad-header-len":     {icoBMPPayload(108, 1, 24, 0), "unexpected BMP header length"},
		"width-overflow":     {icoBMPPayload(40, 536870911, 8, 0), "unable to load ICO image as BMP"},
		"numcolors-overflow": {icoBMPPayload(40, 1, 8, 1<<30), "invalid BMP palette size"},
	} {
		t.Run(name, func(t *testing.T) {
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			defer Cleanup()
			data := imagedata.NewFromBytesWithFormat(imagetype.ICO, icoEntry(tc.payload, uint32(len(tc.payload))))
			defer data.Close()
			img := new(Image)
			defer img.Clear()
			err := img.Load(data, 1, 0, 1)
			if err == nil {
				err = img.CopyMemory()
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}
}
