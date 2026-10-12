// Derived from imgproxy (Copyright imgproxy contributors), Apache-2.0.
// Modified by iXoSS, including import paths and/or implementation.
// See LICENSE-imgproxy, NOTICE and third_party/imgproxy/UPSTREAM.json.

package options

import (
	"fmt"

	"github.com/vndroid/iXoSS/internal/errctx"
)

type (
	TypeMismatchError struct{ *errctx.TextError }
)

func newTypeMismatchError(key string, exp, got any) error {
	return TypeMismatchError{errctx.NewTextError(
		fmt.Sprintf("option %s is %T, not %T", key, exp, got),
		1,
	)}
}
