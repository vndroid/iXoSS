package vips

import (
	"bytes"
	"encoding/binary"
	"image/png"
	"runtime"
	"strconv"
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
