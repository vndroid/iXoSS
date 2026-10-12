// Derived from imgproxy (Copyright imgproxy contributors), Apache-2.0.
// Modified by iXoSS, including import paths and/or implementation.
// See LICENSE-imgproxy, NOTICE and third_party/imgproxy/UPSTREAM.json.

package processing

import (
	"github.com/vndroid/iXoSS/internal/imath"
)

func (p *Processor) padding(c *Context) error {
	if !c.PO.PaddingEnabled() {
		return nil
	}

	paddingTop := imath.ScaleToEven(c.PO.PaddingTop(), c.DprScale)
	paddingRight := imath.ScaleToEven(c.PO.PaddingRight(), c.DprScale)
	paddingBottom := imath.ScaleToEven(c.PO.PaddingBottom(), c.DprScale)
	paddingLeft := imath.ScaleToEven(c.PO.PaddingLeft(), c.DprScale)

	return c.Img.Embed(
		c.Img.Width()+paddingLeft+paddingRight,
		c.Img.Height()+paddingTop+paddingBottom,
		paddingLeft,
		paddingTop,
	)
}
