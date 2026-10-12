// Derived from imgproxy (Copyright imgproxy contributors), Apache-2.0.
// Modified by iXoSS, including import paths and/or implementation.
// See LICENSE-imgproxy, NOTICE and third_party/imgproxy/UPSTREAM.json.

package processing

func (p *Processor) flatten(c *Context) error {
	if !c.PO.ShouldFlatten() && c.PO.Format().SupportsAlpha() {
		return nil
	}

	return c.Img.Flatten(c.PO.Background())
}
