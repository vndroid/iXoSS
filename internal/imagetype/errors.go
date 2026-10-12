// Derived from imgproxy (Copyright imgproxy contributors), Apache-2.0.
// Modified by iXoSS, including import paths and/or implementation.
// See LICENSE-imgproxy, NOTICE and third_party/imgproxy/UPSTREAM.json.

package imagetype

import (
	"net/http"

	"github.com/vndroid/iXoSS/internal/errctx"
)

type (
	TypeDetectionError struct{ *errctx.WrappedError }
	UnknownFormatError struct{ *errctx.TextError }
)

func newTypeDetectionError(err error) error {
	return TypeDetectionError{errctx.NewWrappedError(
		err,
		1,
		errctx.WithPrefix("failed to detect image type"),
		errctx.WithStatusCode(http.StatusUnprocessableEntity),
		errctx.WithPublicMessage("Failed to detect source image type"),
		errctx.WithShouldReport(false),
	)}
}

func newUnknownFormatError() error {
	return UnknownFormatError{errctx.NewTextError(
		"Source image type not supported",
		1,
		errctx.WithStatusCode(http.StatusUnprocessableEntity),
		errctx.WithPublicMessage("Invalid source image"),
		errctx.WithShouldReport(false),
	)}
}
