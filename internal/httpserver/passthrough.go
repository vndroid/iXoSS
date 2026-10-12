package httpserver

import (
	"bytes"
	"io"
	"net/http"

	"github.com/vndroid/iXoSS/internal/imagedata"
	"github.com/vndroid/iXoSS/internal/imageinfo"
	"github.com/vndroid/iXoSS/internal/imagetype"
	"github.com/vndroid/iXoSS/internal/options"
	"github.com/vndroid/iXoSS/internal/ossprocess"
)

// tryServeOriginal runs before any disk cache lookup. Returning false leaves
// processing unchanged when the request changes the image or cannot be proven
// to be an identity operation. The opened object is reused for transmission.
func (s *Server) tryServeOriginal(w http.ResponseWriter, r *http.Request, chain *ossprocess.Chain, negotiated imagetype.Type) bool {
	obj, err := s.src.Open(r.Context(), r.URL.Path)
	if err != nil {
		s.failSource(w, r, err)
		return true
	}
	defer obj.Close()
	if overLimit(obj.Size, s.cfg.MaxSourceBytes) {
		s.failTooLarge(w, r, errSourceTooLarge{size: obj.Size, limit: s.cfg.MaxSourceBytes})
		return true
	}
	header, err := obj.Peek(imageinfo.HeaderBytes)
	if err != nil {
		s.failSource(w, r, err)
		return true
	}
	format, err := imagetype.Detect(bytes.NewReader(header), "", "")
	if err != nil || !chain.PreservesOriginalFormat(format, negotiated) {
		return false
	}
	var width, height int
	var body io.WriterTo = obj
	info, _ := imageinfo.Read(bytes.NewReader(header), obj.Size)
	if info != nil {
		width, height = info.ImageWidth, info.ImageHeight
	}
	if chain.NeedsSourceSize() && (width <= 0 || height <= 0) {
		// Formats without a header parser need a metadata load, not a transcode.
		// Keep the original bytes for transmission because ReadAll consumes obj.
		data, err := readAll(obj, s.cfg.MaxSourceBytes)
		if err != nil {
			s.failFetch(w, r, err)
			return true
		}
		src, err := imagedata.NewFromBytes(data)
		if err != nil {
			return false
		}
		defer src.Close()
		width, height, err = s.sourceSize(data, src)
		if err != nil {
			return false
		}
		body = bytes.NewReader(data)
	}
	if !chain.MatchesOriginal(width, height, format, negotiated) {
		return false
	}
	if s.cfg.HeaderChecker != nil && width > 0 && height > 0 {
		// No animation frames are decoded, but preserve the source pixel limit.
		if err := s.cfg.HeaderChecker.CheckDimensions(options.New(), width, height, 1); err != nil {
			s.failProcess(w, r, err)
			return true
		}
	}
	s.writeOriginal(w, r, &obj.Info, format.Mime(), body)
	return true
}
