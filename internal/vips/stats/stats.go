// Derived from imgproxy (Copyright imgproxy contributors), Apache-2.0.
// Modified by iXoSS, including import paths and/or implementation.
// See LICENSE-imgproxy, NOTICE and third_party/imgproxy/UPSTREAM.json.

package vipsstats

/*
#cgo pkg-config: vips
#include <vips/vips.h>
*/
import "C"

func Memory() float64 {
	return float64(C.vips_tracked_get_mem())
}

func MemoryHighwater() float64 {
	return float64(C.vips_tracked_get_mem_highwater())
}

func Allocs() float64 {
	return float64(C.vips_tracked_get_allocs())
}
