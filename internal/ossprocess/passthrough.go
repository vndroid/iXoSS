package ossprocess

import (
	"github.com/vndroid/iXoSS/internal/imagetype"
	"github.com/vndroid/iXoSS/internal/processing"
)

// CanPassthrough limits the shortcut to chains containing only resize/format.
// Quality, metadata, rotation, crop and watermark actions must still run even
// when the final dimensions and format happen to match the source.
func (c *Chain) CanPassthrough() bool {
	if c.IsEmpty() {
		return false
	}
	for _, a := range c.Actions {
		if a.Name != "resize" && a.Name != "format" {
			return false
		}
	}
	return true
}

// PreservesOriginalFormat resolves explicit/automatic formats without asking
// an encoder to run. No format action means keep the source in this shortcut.
func (c *Chain) PreservesOriginalFormat(source, autoFormat imagetype.Type) bool {
	if !c.CanPassthrough() || source == imagetype.Unknown {
		return false
	}
	for _, a := range c.Actions {
		if a.Name != "format" {
			continue
		}
		format, err := a.format()
		if err != nil {
			return false
		}
		if format == imagetype.Unknown {
			format = autoFormat
			if format == imagetype.Unknown {
				format = source
			}
		}
		// OSS preserves a non-GIF source for format,gif.
		if format != source && format != imagetype.GIF {
			return false
		}
	}
	return true
}

// MatchesOriginal reports whether every requested action leaves the source's
// dimensions and format unchanged. autoFormat is the negotiated format, or
// Unknown when format,auto falls back to the source. Call Validate first.
func (c *Chain) MatchesOriginal(w, h int, source, autoFormat imagetype.Type) bool {
	if !c.PreservesOriginalFormat(source, autoFormat) {
		return false
	}
	for _, a := range c.Actions {
		if a.Name != "resize" {
			continue
		}
		if w <= 0 || h <= 0 {
			return false
		}
		r, err := parseResize(a)
		if err != nil {
			return false
		}
		target := r.Resolve(w, h)
		if target.Noop {
			continue
		}
		if target.Type == processing.ResizeFit && !target.Pad {
			// Fit can use a larger box while leaving one side at scale 1.
			if target.W < w || target.H < h || target.W != w && target.H != h {
				return false
			}
		} else if target.W != w || target.H != h {
			return false
		}
	}
	return true
}
