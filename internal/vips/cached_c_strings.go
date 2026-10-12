// Derived from imgproxy (Copyright imgproxy contributors), Apache-2.0.
// Modified by iXoSS, including import paths and/or implementation.
// See LICENSE-imgproxy, NOTICE and third_party/imgproxy/UPSTREAM.json.

package vips

import "C"
import "sync"

var cStringsCache sync.Map

func cachedCString(str string) *C.char {
	if cstr, ok := cStringsCache.Load(str); ok {
		return cstr.(*C.char) //nolint:forcetypeassert
	}

	cstr := C.CString(str)
	cStringsCache.Store(str, cstr)

	return cstr
}
