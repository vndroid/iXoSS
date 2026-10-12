// Derived from imgproxy (Copyright imgproxy contributors), Apache-2.0.
// Modified by iXoSS, including import paths and/or implementation.
// See LICENSE-imgproxy, NOTICE and third_party/imgproxy/UPSTREAM.json.

package iptc

import (
	"fmt"
	"net/http"

	"github.com/vndroid/iXoSS/internal/errctx"
)

type IptcError struct{ *errctx.TextError }

func newIptcError(format string, args ...any) error {
	return IptcError{errctx.NewTextError(
		fmt.Sprintf(format, args...),
		1,
		errctx.WithStatusCode(http.StatusUnprocessableEntity),
		errctx.WithPublicMessage("Invalid IPTC data"),
		errctx.WithShouldReport(false),
	)}
}
