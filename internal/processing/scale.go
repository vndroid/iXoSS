// Derived from imgproxy (Copyright imgproxy contributors), Apache-2.0.
// Modified by iXoSS, including import paths and/or implementation.
// See LICENSE-imgproxy, NOTICE and third_party/imgproxy/UPSTREAM.json.

package processing

func (p *Processor) scale(c *Context) error {
	if c.WScale == 1 && c.HScale == 1 {
		return nil
	}

	wscale, hscale := c.WScale, c.HScale

	if (c.Angle+c.PO.Rotate())%180 == 90 {
		wscale, hscale = hscale, wscale
	}

	// Save current colorspace
	cs := c.Img.Type()

	// Convert to linear colorspace if needed
	if p.config.UseLinearColorspace {
		// We need this to keep colors consistent after processing
		if err := c.Img.ImportColourProfile(); err != nil {
			return err
		}

		// Convert to linear colorspace
		if err := c.Img.LinearColourspace(); err != nil {
			return err
		}
	}

	if err := c.Img.Resize(wscale, hscale); err != nil {
		return err
	}

	// Convert back to original colorspace if we used linear during processing
	if p.config.UseLinearColorspace {
		return c.Img.Colorspace(cs)
	}

	return nil
}
