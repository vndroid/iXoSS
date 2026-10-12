// Derived from imgproxy (Copyright imgproxy contributors), Apache-2.0.
// Modified by iXoSS, including import paths and/or implementation.
// See LICENSE-imgproxy, NOTICE and third_party/imgproxy/UPSTREAM.json.

package color

import (
	"fmt"

	"github.com/vndroid/iXoSS/internal/errctx"
)

type ColorError struct{ *errctx.TextError }

func newColorError(format string, args ...any) error {
	return ColorError{errctx.NewTextError(
		fmt.Sprintf(format, args...),
		1,
		errctx.WithShouldReport(false),
	)}
}
