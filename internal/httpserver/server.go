// Package httpserver wires the OSS grammar, the source, the cache and the
// ported imgproxy pipeline into an HTTP handler.
//
// Request shape:
//
//	GET /path/to/image.jpg                              -> the source, untouched
//	GET /path/to/image.jpg?x-oss-process=image/format,avif
//	GET /path/to/image.jpg?x-oss-process=image/info     -> JSON
//
// Source images and watermarks are read from the configured root directory.
package httpserver

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/vndroid/iXoSS/internal/auximageprovider"
	"github.com/vndroid/iXoSS/internal/cache"
	"github.com/vndroid/iXoSS/internal/imagedata"
	"github.com/vndroid/iXoSS/internal/imageinfo"
	"github.com/vndroid/iXoSS/internal/imagetype"
	"github.com/vndroid/iXoSS/internal/options"
	"github.com/vndroid/iXoSS/internal/options/keys"
	"github.com/vndroid/iXoSS/internal/ossprocess"
	"github.com/vndroid/iXoSS/internal/processing"
	"github.com/vndroid/iXoSS/internal/singleflight"
	"github.com/vndroid/iXoSS/internal/source"
	"github.com/vndroid/iXoSS/internal/vips"
	"github.com/vndroid/iXoSS/internal/vips/color"
)

// The info handler asks the source for exactly imageinfo's header window, so
// the source's buffer has to be able to hold it. Both are 64 KiB; this fails
// the build rather than the request if one of them ever moves.
const _ = uint(source.HeaderWindow - imageinfo.HeaderBytes)

// Config holds the knobs the HTTP layer itself needs. Image-processing config
// lives in processing.Config / vips.Config.
type Config struct {
	// Root is the directory images are served from. Required.
	Root string

	// CacheDir stores transcoded results. Empty disables caching, which is only
	// sensible for testing — see the package comment in internal/cache.
	CacheDir string
	// PassthroughNonImages allows the original-object endpoint to serve bytes
	// that are not a registered image format. Disabled by default.
	PassthroughNonImages bool
	// StylesFile is a JSON object mapping style names to image processing chains.
	// It is loaded and validated once at startup; changes require a restart.
	StylesFile string
	// MaxSourceBytes rejects source objects larger than this.
	MaxSourceBytes int64

	// MaxWatermarkBytes rejects a watermark file larger than this.
	// Separate from MaxSourceBytes because a watermark is a decoration
	// composited onto an image, not the image, and giving it the source's
	// budget is what made 64 cached watermarks worth 6.25 GiB.
	MaxWatermarkBytes int64
	// MaxWatermarkResolution rejects a watermark of more pixels than this,
	// counted as width x height x frames and checked from the header before
	// anything is decoded. This is the bound that protects the process: bytes
	// and pixels are only loosely related, and a 440 KB PNG that decodes to
	// 144 MP cost a measured 444 MB of RSS for one request.
	MaxWatermarkResolution int
	// WatermarkCacheBytes bounds every retained watermark added together.
	WatermarkCacheBytes int64
	// Concurrency caps simultaneous image processing. Zero means GOMAXPROCS.
	Concurrency int
	// ProcessTimeout bounds one transcode, not counting the local file read.
	ProcessTimeout time.Duration
	// CacheControl is sent with successful image responses.
	CacheControl string
	// Evict bounds the disk cache. Zero Interval leaves it unbounded.
	Evict cache.EvictConfig
	// Verifier checks request signatures. Nil, or one that reports signing is
	// unconfigured, serves every request unsigned.
	Verifier SignatureVerifier
	// HeaderChecker applies source limits when a cold HEAD can determine the
	// geometry from its bounded header window.
	HeaderChecker HeaderChecker
}

// HeaderChecker is the subset of the processing security policy needed by a
// cheap processed HEAD. *security.Checker implements it.
type HeaderChecker interface {
	MaxAnimationFrames(*options.Options) int
	CheckAnimationFrames(*options.Options, int) error
	CheckDimensions(*options.Options, int, int, int) error
}

// SignatureVerifier checks that a request was issued by someone holding the
// shared key. *security.Checker implements it.
type SignatureVerifier interface {
	// SignatureEnabled reports whether a key and salt are configured. When it is
	// false the server does not ask for a signature at all.
	SignatureEnabled() bool
	// VerifySignature returns nil if signature is valid for message.
	VerifySignature(ctx context.Context, signature, message string) error
}

// SignatureQueryKey is the query parameter carrying a request's signature.
//
// imgproxy signs in the path, because its path *is* the request: options and
// source URL are both segments of it. iXoSS's path is the object key and
// nothing else, so a signature segment would have to be spliced in front of a
// real filename. A query parameter leaves the key alone.
const SignatureQueryKey = "x-ixoss-signature"

// Watermark budgets. See the watermarkProvider doc comment for why there are
// three of them and what each one actually stops.
const (
	// DefaultMaxWatermarkBytes is one watermark on the wire. Generous for a
	// logo, and small enough that the cache budget below holds several.
	DefaultMaxWatermarkBytes = 16 << 20

	// DefaultMaxWatermarkResolution is one watermark in pixels.
	//
	// 8 MP is a 2828x2828 square or a 4000x2000 banner — far more than any
	// real watermark needs, and 31x below the source image's 250 MP budget,
	// which is the ceiling this replaces. Measured cost scales at roughly
	// 3 MB of RSS per megapixel, so 8 MP is about 25 MB per concurrent use;
	// the 144 MP image that prompted this was 444 MB. Raise it if a deployment
	// genuinely composites something huge, and multiply by IXOSS_CONCURRENCY
	// when deciding how far.
	DefaultMaxWatermarkResolution = 8_000_000

	// DefaultWatermarkCacheBytes is every retained watermark added together.
	DefaultWatermarkCacheBytes = 64 << 20
)

// NewDefaultConfig returns usable defaults.
func NewDefaultConfig() Config {
	return Config{
		MaxSourceBytes: 100 << 20,
		ProcessTimeout: 20 * time.Second,
		CacheControl:   "public, max-age=300",

		MaxWatermarkBytes:      DefaultMaxWatermarkBytes,
		MaxWatermarkResolution: DefaultMaxWatermarkResolution,
		WatermarkCacheBytes:    DefaultWatermarkCacheBytes,
	}
}

// Server is the HTTP handler.
type Server struct {
	cfg    Config
	src    source.Source
	cache  *cache.Disk
	proc   *processing.Processor
	styles map[string]string

	// sem bounds concurrent encodes. libvips is happy to use every core for one
	// image; letting N requests each do that turns a small box into a queue with
	// extra steps.
	sem    chan struct{}
	flight singleflight.Group

	// watermarks is handed to the processor at construction time; it reads the
	// requested object key out of each request's options.
	watermarks *watermarkProvider

	// verifier is nil unless a key and salt are configured, so the check costs
	// one nil comparison on the unsigned deployment.
	verifier SignatureVerifier

	stop chan struct{}
}

// New builds a Server.
//
// newProcessor is called with the watermark provider once the source is open,
// because the provider needs the source: an OSS watermark names an object key,
// and that key is fetched exactly the way the image being served is — through
// the same traversal and symlink checks on disk.
func New(cfg Config, newProcessor func(auximageprovider.Provider) (*processing.Processor, error)) (*Server, error) {
	stop := make(chan struct{})
	styles, err := loadStyles(cfg.StylesFile)
	if err != nil {
		return nil, err
	}

	src, err := source.NewLocal(cfg.Root)
	if err != nil {
		return nil, err
	}

	watermarks := newWatermarkProvider(src, cfg)
	proc, err := newProcessor(watermarks)
	if err != nil {
		return nil, err
	}

	var c *cache.Disk
	if cfg.CacheDir != "" {
		if c, err = cache.NewDisk(cfg.CacheDir); err != nil {
			return nil, err
		}
		c.StartEvictor(cfg.Evict, stop)
	}

	n := cfg.Concurrency
	if n <= 0 {
		n = 1
	}

	var verifier SignatureVerifier
	if cfg.Verifier != nil && cfg.Verifier.SignatureEnabled() {
		verifier = cfg.Verifier
	}

	return &Server{
		cfg:        cfg,
		src:        src,
		cache:      c,
		proc:       proc,
		styles:     styles,
		sem:        make(chan struct{}, n),
		watermarks: watermarks,
		verifier:   verifier,
		stop:       stop,
	}, nil
}

// SourceDescription names where objects come from, for the startup log.
func (s *Server) SourceDescription() string { return s.src.Describe() }

// Close stops the cache evictor and releases cached watermarks.
func (s *Server) Close() error {
	close(s.stop)
	return s.watermarks.Close()
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		s.fail(w, r, http.StatusMethodNotAllowed, "MethodNotAllowed", "only GET and HEAD are supported")
		return
	}

	// /healthz is exempt: it reports whether libvips is alive and reveals nothing
	// about the objects being served, and a load balancer has no key.
	normalizedPath := collapseSlashes(r.URL.Path)
	query, queryChanged, err := filterQuery(r.URL.RawQuery)
	if err != nil {
		s.fail(w, r, http.StatusBadRequest, "InvalidArgument", "invalid URL query")
		return
	}
	if normalizedPath != "/healthz" {
		if err := s.verifySignature(r); err != nil {
			s.failSignature(w, r, err)
			return
		}
	}
	var chain *ossprocess.Chain
	var effective string
	if normalizedPath != "/healthz" {
		raw := r.URL.Query().Get(ossprocess.QueryKey)
		// Signatures cover the public alias, not the configured expansion. Keep the
		// effective chain separate so a style edit changes the cache key on restart.
		effective, err = expandStyle(raw, s.styles)
		if err != nil {
			s.fail(w, r, http.StatusBadRequest, "InvalidArgument", err.Error())
			return
		}
		chain, err = ossprocess.Parse(effective)
		if err != nil {
			s.fail(w, r, http.StatusBadRequest, "InvalidArgument", err.Error())
			return
		}
		if err := chain.Validate(); err != nil {
			s.fail(w, r, http.StatusBadRequest, "InvalidArgument", err.Error())
			return
		}
	}
	if normalizedPath != r.URL.Path || queryChanged {
		// Escape the path, but retain validated query fragments verbatim. The
		// origin-relative Location cannot redirect to a different host.
		target := &url.URL{Path: normalizedPath, RawQuery: query}
		w.Header().Set("Location", target.RequestURI())
		w.WriteHeader(http.StatusPermanentRedirect)
		return
	}
	if normalizedPath == "/healthz" {
		s.health(w)
		return
	}

	switch {
	case chain.IsInfo():
		s.serveInfo(w, r)
	case chain.IsAverageHue():
		s.serveAverageHue(w, r)
	case chain.IsEmpty():
		s.serveOriginal(w, r)
	default:
		s.serveProcessed(w, r, chain, effective)
	}
}

// filterQuery validates URI query syntax and escapes before retaining supported
// pairs in their original order and spelling. It never reinserts decoded data
// into Location, so encoded separators remain encoded.
func filterQuery(raw string) (string, bool, error) {
	for i := range len(raw) {
		c := raw[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
			strings.ContainsRune("-._~!$&'()*+,;=:@/?%", rune(c))) {
			return "", false, fmt.Errorf("invalid query character")
		}
	}
	if _, err := url.ParseQuery(raw); err != nil {
		return "", false, err
	}
	var kept []string
	for _, pair := range strings.Split(raw, "&") {
		if pair == "" {
			continue
		}
		key, _, _ := strings.Cut(pair, "=")
		name, err := url.QueryUnescape(key)
		if err != nil {
			return "", false, err
		}
		if name != ossprocess.QueryKey && name != SignatureQueryKey {
			return "", false, fmt.Errorf("unsupported query parameter %q", name)
		}
		kept = append(kept, pair)
	}
	filtered := strings.Join(kept, "&")
	return filtered, filtered != raw, nil
}

// collapseSlashes preserves all path segments, including dots and a trailing
// slash, while reducing each run of slashes to a single separator.
func collapseSlashes(path string) string {
	for strings.Contains(path, "//") {
		path = strings.ReplaceAll(path, "//", "/")
	}
	return path
}

// ----------------------------------------------------------------- signature

// SignedMessage is what a signature covers: the object path with repeated
// slashes collapsed, plus the process chain's decoded query value unchanged.
//
//	/photo.jpg
//	/photo.jpg?x-oss-process=image/resize,w_800
//
// Both halves have to be in it. Signing the path alone would let anyone rewrite
// the transform on a URL they were handed, and an unbounded chain of resizes is
// a cheap way to spend someone else's CPU; signing the chain alone would let
// them point it at a different object. Other query parameters are outside the
// signature deliberately — cache-busters and analytics tags are added by things
// that do not have the key.
func SignedMessage(path, chain string) string {
	path = collapseSlashes(path)
	if chain == "" {
		return path
	}
	return path + "?" + ossprocess.QueryKey + "=" + chain
}

// verifySignature checks the request's x-ixoss-signature parameter.
//
// It is a no-op until IXOSS_KEY and IXOSS_SALT are both set: New leaves
// s.verifier nil otherwise, so an unsigned deployment behaves exactly as it did
// before signing existed.
func (s *Server) verifySignature(r *http.Request) error {
	if s.verifier == nil {
		return nil
	}

	q := r.URL.Query()
	msg := SignedMessage(r.URL.Path, q.Get(ossprocess.QueryKey))

	return s.verifier.VerifySignature(r.Context(), q.Get(SignatureQueryKey), msg)
}

// failSignature answers a rejected signature.
//
// The body says only "Forbidden". Which of "no signature", "wrong key" and
// "signature for a different path" it was is information a prober would use to
// work out what iXoSS signs, and the operator gets the real reason in the log.
func (s *Server) failSignature(w http.ResponseWriter, r *http.Request, err error) {
	slog.Debug("signature rejected", "path", r.URL.Path, "error", err)

	status := http.StatusForbidden
	var coded interface{ StatusCode() int }
	if errors.As(err, &coded) {
		status = coded.StatusCode()
	}

	s.fail(w, r, status, "AccessDenied", "Forbidden")
}

// ------------------------------------------------------------------ original

// originalSniffBytes uses the detector's full window so an SVG with a long
// preamble is not mistaken for a non-image in strict mode.
const originalSniffBytes = 32 << 10

func (s *Server) serveOriginal(w http.ResponseWriter, r *http.Request) {
	// Opening a file is cheap; Peek below reads only the format detection window.
	obj, err := s.src.Open(r.Context(), r.URL.Path)
	if err != nil {
		s.failSource(w, r, err)
		return
	}
	defer obj.Close()

	// Detect the format from the bytes. Peek preserves them for the copy below.
	b, perr := obj.Peek(originalSniffBytes)
	if perr != nil {
		s.failSource(w, r, perr)
		return
	}
	mime := "application/octet-stream"
	if t, terr := imagetype.Detect(bytes.NewReader(b), "", ""); terr == nil {
		mime = t.Mime()
	} else if !s.cfg.PassthroughNonImages {
		s.fail(w, r, http.StatusUnsupportedMediaType, "InvalidArgument", "source is not a supported image")
		return
	}
	s.writeOriginal(w, r, &obj.Info, mime, obj)
}

// writeOriginal shares headers and conditional requests between the original
// endpoint and resize/format requests that can transmit the source unchanged.
func (s *Server) writeOriginal(w http.ResponseWriter, r *http.Request, info *source.Info, mime string, body io.WriterTo) {
	w.Header().Set("X-IXoSS-Cache", "BYPASS")
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size, 10))
	w.Header().Set("Cache-Control", s.cfg.CacheControl)
	// File identity is a weak validator, not a digest of the response bytes.
	etag := `W/"` + s.cacheKey(info, "original").Hash() + `"`
	if notModified(w, r, etag) {
		return
	}
	if r.Method == http.MethodHead {
		return
	}
	if _, err := body.WriteTo(w); err != nil {
		slog.Debug("write failed", "path", r.URL.Path, "error", err)
	}
}

// --------------------------------------------------------------- average-hue

// serveAverageHue answers `x-oss-process=image/average-hue` with the image's
// mean colour as `0xRRGGBB` in plain text — OSS returns it bare, not wrapped in
// JSON the way info is.
//
// Unlike info this needs the pixels, so it takes a concurrency slot and decodes
// the image. It is still far cheaper than a transform: one decode, no resize, no
// encode, and a seven-byte body.
func (s *Server) serveAverageHue(w http.ResponseWriter, r *http.Request) {
	// The decode below reads the whole object into memory, so this endpoint is
	// bounded by the same limit as a transform. It used to have no bound at all,
	// which made it the cheapest way to ask the process to allocate 2 GB.
	b, err := s.fetchAll(r.Context(), r.URL.Path, s.cfg.MaxSourceBytes)
	if err != nil {
		s.failFetch(w, r, err)
		return
	}

	hue, err := s.averageHue(b)
	if err != nil {
		s.fail(w, r, http.StatusUnprocessableEntity, "InvalidImage", "the image could not be read")
		return
	}

	body := fmt.Sprintf("0x%02x%02x%02x", hue.R, hue.G, hue.B)

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.Header().Set("Cache-Control", s.cfg.CacheControl)
	if notModified(w, r, contentETag([]byte(body))) {
		return
	}
	if r.Method == http.MethodHead {
		return
	}
	w.Write([]byte(body))
}

// averageHue decodes one frame and returns its mean colour.
func (s *Server) averageHue(b []byte) (color.RGB, error) {
	s.acquire()
	defer s.release()

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer vips.Cleanup()

	data, err := imagedata.NewFromBytes(b)
	if err != nil {
		return color.RGB{}, err
	}
	defer data.Close()

	img := new(vips.Image)
	defer img.Clear()

	// One frame is enough: an animation's mean colour is not worth decoding
	// every frame for, and OSS reports a single value too.
	if err := img.Load(data, 1.0, 0, 1); err != nil {
		return color.RGB{}, err
	}

	return img.AverageHue()
}

// ---------------------------------------------------------------------- info

func (s *Server) serveInfo(w http.ResponseWriter, r *http.Request) {
	obj, err := s.src.Open(r.Context(), r.URL.Path)
	if err != nil {
		s.failSource(w, r, err)
		return
	}
	defer obj.Close()

	head, herr := obj.Peek(imageinfo.HeaderBytes)
	if herr != nil {
		s.fail(w, r, http.StatusUnprocessableEntity, "InvalidImage", "the image could not be read")
		return
	}

	info, err := imageinfo.Read(bytes.NewReader(head), obj.Size)
	if info == nil {
		s.fail(w, r, http.StatusUnprocessableEntity, "InvalidImage", "the image could not be read")
		return
	}

	// imageinfo parses JPEG, PNG, GIF and WebP headers directly, which is the
	// cheap path and answers most requests outright. Three things send a request
	// to libvips instead, and they are three different situations:
	//
	//   - a container imageinfo has no walk for (AVIF, HEIC, JXL, TIFF, BMP);
	//   - a JPEG whose SOF sits past the 64 KiB window, which a big EXIF
	//     thumbnail or a segmented ICC profile is enough to cause;
	//   - an animated WebP, or a GIF whose blocks outrun the window, where the
	//     geometry is known but the frame count is only a floor.
	//
	// The first two have no answer without libvips. The third has a usable one
	// already, so its fallback is best-effort: if it cannot run, the floor is
	// still served.
	//
	// Note what is *not* on that list: a missing Exif map. It is tempting to add
	// it — that is where AVIF's EXIF comes from — but "this image has no EXIF" is
	// indistinguishable from "EXIF has not been looked for yet", and a JPEG
	// without EXIF is the single most common request there is. Sending those to
	// libvips reads every one of them off disk in full: measured at +80 MB of RSS
	// across five info requests for one 40 MB JPEG that the header walk had
	// already answered from its first 64 KiB. So EXIF is only ever filled in on a
	// trip that was happening anyway, inside infoViaVips.
	var unsupported imageinfo.ErrUnsupportedContainer
	needsGeometry := errors.As(err, &unsupported) || errors.Is(err, imageinfo.ErrHeaderTooShort)

	if needsGeometry || !info.FrameCountKnown {
		refined, rerr := s.infoViaVips(obj, info)
		switch {
		case rerr == nil:
			info, err = refined, nil
		case needsGeometry:
			// Nothing else was going to supply the dimensions.
			err = rerr
		default:
			// Keep what the header walk found and drop the refinement.
			slog.Debug("info refinement failed", "path", r.URL.Path, "error", rerr)
			err = nil
		}
	}
	var big errSourceTooLarge
	if errors.As(err, &big) {
		s.failTooLarge(w, r, big)
		return
	}
	if err != nil {
		s.fail(w, r, http.StatusUnprocessableEntity, "InvalidImage", err.Error())
		return
	}

	body, err := json.Marshal(info)
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, "InternalError", "encoding failed")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.Header().Set("Cache-Control", s.cfg.CacheControl)
	if notModified(w, r, contentETag(body)) {
		return
	}
	if r.Method == http.MethodHead {
		return
	}
	w.Write(body)
}

// infoViaVips fills in whatever the header walk could not, and only that.
//
// Each field is taken from libvips on its own condition, because the three
// callers want different subsets and there is no reason to overwrite a value
// that was read from the container itself. `partial` already carries the two
// fields that are right regardless — file size, and the format from the magic
// bytes — so they are never touched here.
//
// This reads the open file under MaxSourceBytes when its header is insufficient.
func (s *Server) infoViaVips(obj *source.Object, partial *imageinfo.Info) (*imageinfo.Info, error) {
	limit := s.cfg.MaxSourceBytes

	if overLimit(obj.Size, limit) {
		return nil, errSourceTooLarge{size: obj.Size, limit: limit}
	}

	b, err := readAll(obj, limit)
	if err != nil {
		return nil, err
	}

	data, err := imagedata.NewFromBytes(b)
	if err != nil {
		return nil, err
	}
	defer data.Close()

	s.acquire()
	defer s.release()

	img := new(vips.Image)
	defer img.Clear()

	if err := img.Load(data, 1.0, 0, 1); err != nil {
		return nil, err
	}

	out := *partial

	// Zero width is how Read reports that it never got as far as the geometry.
	if out.ImageWidth == 0 {
		out.ImageWidth = img.Width()
		out.ImageHeight = img.PageHeight()
	}

	// Pages() reads the container's own page count, not the number of frames
	// this load pulled in — the load above asks for one — so it is the real
	// answer for an animated WebP or a GIF longer than the header window.
	if !out.FrameCountKnown {
		out.FrameCount = img.Pages()
		out.FrameCountKnown = true
	}

	// The EXIF libvips found, for the containers whose walk this package does
	// not have: AVIF and HEIC keep it in an ISOBMFF item, JXL in a box, and both
	// come back here as the same bare TIFF block a PNG carries in eXIf. Without
	// this the response has eight fields and — worse — reports the *defaults*
	// for XResolution, YResolution and ResolutionUnit, which is not a gap in the
	// answer but a wrong one.
	if out.Exif == nil {
		if b, err := img.GetBlob(vips.MetaEXIF); err == nil && len(b) > 0 {
			imageinfo.ApplyEXIF(&out, b)
		}
	}

	return &out, nil
}

// ----------------------------------------------------------------- processed

func (s *Server) serveProcessed(w http.ResponseWriter, r *http.Request, chain *ossprocess.Chain, raw string) {
	info, err := s.src.Stat(r.Context(), r.URL.Path)
	if err != nil {
		s.failSource(w, r, err)
		return
	}
	if overLimit(info.Size, s.cfg.MaxSourceBytes) {
		s.failTooLarge(w, r, errSourceTooLarge{size: info.Size, limit: s.cfg.MaxSourceBytes})
		return
	}

	// format,auto makes the output depend on the client, so the negotiated
	// choice has to be part of the cache key — otherwise the first visitor's
	// format is served to everyone.
	auto := chain.IsAutoFormat()
	accept := ""
	negotiated := imagetype.Unknown
	if auto {
		negotiated = negotiateFormat(r.Header.Get("Accept"), vips.SupportsSave)
		accept = acceptKey(negotiated)
		varyOnAccept(w)
	}

	if chain.CanPassthrough() && s.tryServeOriginal(w, r, chain, negotiated) {
		return
	}

	// A matching request needs no encoder; actual transformations still do.
	if err := chain.CheckEncoders(); err != nil {
		s.fail(w, r, http.StatusBadRequest, "InvalidArgument", err.Error())
		return
	}

	key := s.cacheKey(info, raw+"\x00"+accept)

	if s.cache != nil {
		if e, err := s.cache.Get(key); err == nil {
			defer e.Close()
			s.writeImage(w, r, e, e.Size, "", "HIT")
			return
		} else if !errors.Is(err, cache.ErrMiss) {
			slog.Warn("cache read failed", "error", err)
		}
	}
	if r.Method == http.MethodHead {
		s.serveProcessedHeadMiss(w, r, chain, negotiated)
		return
	}

	// Collapse duplicate work: a page referencing the same transform twelve
	// times should cost one encode, not twelve.
	res, err, shared := s.flight.Do(key.Hash(), func() (any, error) {
		return s.process(r.Context(), r.URL.Path, chain, negotiated)
	})
	if err != nil {
		s.failProcess(w, r, err)
		return
	}

	out := res.(*processed)
	status := "MISS"
	if shared {
		status = "COALESCED"
	}

	if s.cache != nil && !shared {
		if err := s.cache.Put(key, out.data); err != nil {
			// Serving is not blocked by a cache write failure.
			slog.Warn("cache write failed", "error", err)
		}
	}

	s.writeImageBytes(w, r, out.data, out.mime, status)
}

// A cache miss on HEAD must not turn into an entire fetch and encode just to
// discover Content-Length. Only advertise a type when the chain fixes it;
// otherwise it depends on decoded alpha/animation and encoder preferences.
func (s *Server) serveProcessedHeadMiss(w http.ResponseWriter, r *http.Request, chain *ossprocess.Chain, negotiated imagetype.Type) {
	obj, err := s.src.Open(r.Context(), r.URL.Path)
	if err != nil {
		s.failSource(w, r, err)
		return
	}
	defer obj.Close()
	if overLimit(obj.Size, s.cfg.MaxSourceBytes) {
		s.failTooLarge(w, r, errSourceTooLarge{size: obj.Size, limit: s.cfg.MaxSourceBytes})
		return
	}
	b, err := obj.Peek(originalSniffBytes)
	if err != nil {
		s.failSource(w, r, err)
		return
	}
	if _, err := imagetype.Detect(bytes.NewReader(b), "", ""); err != nil {
		s.fail(w, r, http.StatusUnprocessableEntity, "InvalidArgument", "source is not a supported image")
		return
	}
	if !s.preflightProcessedHead(w, r, obj, b, chain, negotiated) {
		return
	}
	if t, ok := chain.FixedOutputFormat(); ok {
		w.Header().Set("Content-Type", t.Mime())
	}
	w.Header().Set("Cache-Control", s.cfg.CacheControl)
	w.Header().Set("X-IXoSS-Cache", "MISS")
	if notModified(w, r, "") {
		return
	}
	w.WriteHeader(http.StatusOK)
}

// preflightProcessedHead rejects only facts established by the available
// prefix. A missing JPEG SOF is not a failure while source bytes remain, and
// an incomplete GIF count is only a lower bound.
func (s *Server) preflightProcessedHead(w http.ResponseWriter, r *http.Request, obj *source.Object, header []byte, chain *ossprocess.Chain, negotiated imagetype.Type) bool {
	info, err := imageinfo.Read(bytes.NewReader(header), obj.Size)
	if err != nil {
		if info != nil && info.Format == imagetype.JPEG &&
			errors.Is(err, imageinfo.ErrHeaderTooShort) &&
			obj.Size >= 0 && obj.Size <= int64(len(header)) {
			s.fail(w, r, http.StatusUnprocessableEntity, "InvalidImage", "the JPEG header is incomplete")
			return false
		}
		return true
	}
	if info == nil || s.cfg.HeaderChecker == nil || info.ImageWidth <= 0 || info.ImageHeight <= 0 {
		return true
	}
	o := options.New()
	if err := chain.Apply(o, info.ImageWidth, info.ImageHeight, info.Format); err != nil {
		s.failProcess(w, r, err)
		return false
	}
	if chain.IsAutoFormat() {
		setAutoFormat(o, info.Format, negotiated)
	}
	// A format the operator has GET pass through untouched is still measured,
	// but only as the single frame GET loaded to decide that: no frame cap, and
	// a pixel budget of width × height. So skipping turns off the animation
	// accounting below and nothing else — returning early here would let HEAD
	// wave through a source whose first frame alone is over budget.
	skipped := s.proc != nil && s.proc.SkipsProcessing(o, info.Format)
	frames := 1
	if !skipped && info.FrameCount > 1 && s.cfg.HeaderChecker.MaxAnimationFrames(o) > 1 &&
		decodesAnimation(info.Format) && s.producesAnimation(o, info.Format) {
		if err := s.cfg.HeaderChecker.CheckAnimationFrames(o, info.FrameCount); err != nil {
			s.failProcess(w, r, err)
			return false
		}
		frames = info.FrameCount
	}
	if !info.Format.IsVector() {
		if err := s.cfg.HeaderChecker.CheckDimensions(o, info.ImageWidth, info.ImageHeight, frames); err != nil {
			s.failProcess(w, r, err)
			return false
		}
	}
	return true
}

// producesAnimation asks the processor whether GET would write this source as
// an animation. That depends on more than a named format — with none, or with
// format,auto, it follows the source and IXOSS_PREFERRED_FORMATS — so the
// processor answers rather than a second copy of its rules living here. The
// fallback, for a server built without a processor, only trusts a named format.
func (s *Server) producesAnimation(o *options.Options, source imagetype.Type) bool {
	if s.proc != nil {
		return s.proc.ProducesAnimation(o, source)
	}
	return options.Get(o, keys.Format, imagetype.Unknown).SupportsAnimationSave()
}

// decodesAnimation reports whether libvips reads a container's frames. Only for
// those does GET hold the source to the frame cap and multiply its pixel budget
// by the frame count. APNG is the case that matters: iXoSS's header parser sees
// its frames and this libvips does not, so GET treats it as a still (or refuses
// it for a different reason), and counting its frames here could refuse what
// GET would serve.
func decodesAnimation(t imagetype.Type) bool {
	return t == imagetype.GIF || t == imagetype.WEBP
}

// cacheKey preserves the local cache identity: path, size, mtime and chain.
func (s *Server) cacheKey(info *source.Info, chain string) cache.Key {
	return cache.Key{
		SourcePath: info.Key,
		SourceSize: info.Size,
		SourceMod:  info.ModTime,
		Chain:      chain,
	}
}

type processed struct {
	data []byte
	mime string
}

// process fetches the source and runs the pipeline over it.
//
// ProcessTimeout bounds the encode after the local file has been read.
func (s *Server) process(ctx context.Context, urlPath string, chain *ossprocess.Chain, negotiated imagetype.Type) (*processed, error) {
	b, err := s.fetchAll(ctx, urlPath, s.cfg.MaxSourceBytes)
	if err != nil {
		return nil, err
	}

	src, err := imagedata.NewFromBytes(b)
	if err != nil {
		return nil, err
	}
	defer src.Close()

	// Resolve resize rules from the bytes already in memory.
	var srcW, srcH int
	if chain.NeedsSourceSize() {
		if srcW, srcH, err = s.sourceSize(b, src); err != nil {
			return nil, err
		}
	}

	if s.cfg.ProcessTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, s.cfg.ProcessTimeout)
		defer cancel()
	}

	o := options.New()
	if err := chain.Apply(o, srcW, srcH, src.Format()); err != nil {
		return nil, err
	}
	if chain.IsAutoFormat() {
		setAutoFormat(o, src.Format(), negotiated)
	}

	s.acquire()
	defer s.release()

	res, err := s.proc.ProcessImage(ctx, src, o)
	if err != nil {
		return nil, err
	}
	defer res.OutData.Close()

	// Copy out of the libvips-owned buffer: OutData.Close() frees it, and the
	// bytes outlive this function in the cache and the response.
	out := append([]byte(nil), imagedata.Bytes(res.OutData)...)

	return &processed{data: out, mime: res.OutData.Format().Mime()}, nil
}

// setAutoFormat resolves format,auto for one request. The processed HEAD calls
// it too, so that what it predicts about the output is what GET will encode.
func setAutoFormat(o *options.Options, source, negotiated imagetype.Type) {
	if negotiated == imagetype.Unknown {
		// With no acceptable modern format, format,auto means preserve the
		// source format rather than fall through to process-wide preferences
		// — but only when this build can write it.
		if f := autoFallbackFormat(source, vips.SupportsSave); f != imagetype.Unknown {
			o.Set(keys.Format, f)
		}
		return
	}
	applyAutoFormat(o, negotiated)
}

// sourceSize reads the source's pixel dimensions as cheaply as the format
// allows: a header parse for JPEG/PNG/GIF/WebP, a libvips header load otherwise.
func (s *Server) sourceSize(b []byte, data imagedata.ImageData) (int, int, error) {
	info, err := imageinfo.Read(bytes.NewReader(b), int64(len(b)))

	// Same two fallback conditions as the info handler, for the same reasons: a
	// container with no header walk, or a JPEG whose SOF is past the window.
	// Missing the second one here was worse than missing it there — it turned a
	// plain `resize` on an ordinary camera JPEG into a 422, because resize is
	// exactly the action that needs the source dimensions.
	var unsupported imageinfo.ErrUnsupportedContainer
	if errors.As(err, &unsupported) || errors.Is(err, imageinfo.ErrHeaderTooShort) {
		s.acquire()
		defer s.release()

		img := new(vips.Image)
		defer img.Clear()
		if lerr := img.Load(data, 1.0, 0, 1); lerr != nil {
			return 0, 0, lerr
		}
		return img.Width(), img.PageHeight(), nil
	}
	if err != nil {
		return 0, 0, err
	}
	return info.ImageWidth, info.ImageHeight, nil
}

// ------------------------------------------------------------------- helpers

// fetchAll opens an object and reads it into memory under a hard bound.
//
// Check the stat size before reading and enforce the bound again while reading,
// in case the file grows after stat.
func (s *Server) fetchAll(ctx context.Context, urlPath string, limit int64) ([]byte, error) {
	obj, err := s.src.Open(ctx, urlPath)
	if err != nil {
		return nil, err
	}
	defer obj.Close()

	return readAll(obj, limit)
}

// overLimit reports whether a declared size is over the bound. Zero or negative
// means no bound.
func overLimit(size, limit int64) bool {
	return limit > 0 && size > limit
}

// readAll drains an already-open object under the same bound, translating the
// source's refusal into the one the HTTP layer knows how to answer with.
func readAll(obj *source.Object, limit int64) ([]byte, error) {
	if overLimit(obj.Size, limit) {
		return nil, errSourceTooLarge{size: obj.Size, limit: limit}
	}

	b, err := obj.ReadAll(limit)
	if errors.Is(err, source.ErrTooLarge) {
		// The file grew after stat; report the read limit without a stale size.
		return nil, errSourceTooLarge{size: -1, limit: limit}
	}
	if err != nil {
		return nil, err
	}
	return b, nil
}

func (s *Server) failTooLarge(w http.ResponseWriter, r *http.Request, e errSourceTooLarge) {
	s.fail(w, r, http.StatusRequestEntityTooLarge, "SourceTooLarge", e.Error())
}

// failFetch answers an error that could have come from either the fetch or the
// size bound, which is the shape every whole-object handler has.
func (s *Server) failFetch(w http.ResponseWriter, r *http.Request, err error) {
	var big errSourceTooLarge
	if errors.As(err, &big) {
		s.failTooLarge(w, r, big)
		return
	}
	s.failSource(w, r, err)
}

// errSourceTooLarge carries the refusal out of a helper that cannot write the
// response itself. failProcess and the info handler turn it back into a 413.
// A negative size means the object never declared one.
type errSourceTooLarge struct{ size, limit int64 }

func (e errSourceTooLarge) Error() string {
	if e.size < 0 {
		return fmt.Sprintf("source exceeds the %d byte limit", e.limit)
	}
	return fmt.Sprintf("source is %d bytes, limit is %d", e.size, e.limit)
}

func (e errSourceTooLarge) StatusCode() int { return http.StatusRequestEntityTooLarge }

// acquire/release bound concurrent libvips work.
func (s *Server) acquire() { s.sem <- struct{}{} }
func (s *Server) release() { <-s.sem }

func (s *Server) writeImage(w http.ResponseWriter, r *http.Request, e *cache.Entry, size int64, mime, cacheStatus string) {
	if mime == "" {
		if t, err := imagetype.Detect(e.File, "", ""); err == nil {
			mime = t.Mime()
		}
		e.Seek(0, 0)
	}
	// Hash the cached bytes without buffering the image in memory.
	digest := sha256.New()
	if _, err := io.Copy(digest, e); err != nil {
		s.fail(w, r, http.StatusInternalServerError, "InternalError", "could not read cached image")
		return
	}
	if _, err := e.Seek(0, io.SeekStart); err != nil {
		s.fail(w, r, http.StatusInternalServerError, "InternalError", "could not read cached image")
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	w.Header().Set("Cache-Control", s.cfg.CacheControl)
	w.Header().Set("X-IXoSS-Cache", cacheStatus)
	if notModified(w, r, fmt.Sprintf(`"%x"`, digest.Sum(nil))) {
		return
	}
	if r.Method == http.MethodHead {
		return
	}
	e.WriteTo(w)
}

func (s *Server) writeImageBytes(w http.ResponseWriter, r *http.Request, b []byte, mime, cacheStatus string) {
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Content-Length", strconv.Itoa(len(b)))
	w.Header().Set("Cache-Control", s.cfg.CacheControl)
	w.Header().Set("X-IXoSS-Cache", cacheStatus)
	if notModified(w, r, contentETag(b)) {
		return
	}
	if r.Method == http.MethodHead {
		return
	}
	w.Write(b)
}

func (s *Server) health(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if err := vips.Health(); err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte("ok\n"))
}

// ossError mirrors the error envelope OSS returns, so a client written against
// OSS can parse iXoSS's failures with the same code.
type ossError struct {
	Code    string `json:"Code"`
	Message string `json:"Message"`
}

func (s *Server) fail(w http.ResponseWriter, r *http.Request, status int, code, msg string) {
	slog.Debug("request failed", "path", r.URL.Path, "status", status, "code", code, "message", msg)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Del("ETag")
	body, _ := json.Marshal(ossError{Code: code, Message: msg})
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		w.Write(body)
	}
}

func (s *Server) failSource(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, source.ErrNotFound):
		s.fail(w, r, http.StatusNotFound, "NoSuchKey", "the specified key does not exist")
	case errors.Is(err, source.ErrOutsideRoot), errors.Is(err, source.ErrNotRegular):
		// Deliberately indistinguishable from a miss: telling a prober that a
		// path exists but is out of bounds maps the filesystem for them.
		s.fail(w, r, http.StatusNotFound, "NoSuchKey", "the specified key does not exist")
	default:
		slog.Error("source error", "path", r.URL.Path, "error", err)
		s.fail(w, r, http.StatusInternalServerError, "InternalError", "could not read the source object")
	}
}

func (s *Server) failProcess(w http.ResponseWriter, r *http.Request, err error) {
	var panicErr singleflight.PanicError
	if errors.As(err, &panicErr) {
		slog.Error("coalesced image processing panicked", "path", r.URL.Path, "error", err,
			"stack", string(panicErr.Stack))
		s.fail(w, r, http.StatusInternalServerError, "InternalError", "image processing failed")
		return
	}
	// An argument the parser could only check against the source (indexcrop's
	// slice index) is still the caller's mistake, so it gets 400 and the real
	// message rather than a generic "could not be processed".
	var argErr ossprocess.ArgumentError
	if errors.As(err, &argErr) {
		s.fail(w, r, http.StatusBadRequest, "InvalidArgument", argErr.Error())
		return
	}
	if errors.Is(err, ErrBadWatermark) {
		s.fail(w, r, http.StatusBadRequest, "InvalidArgument", ErrBadWatermark.Error())
		return
	}
	if errors.Is(err, ErrWatermarkTooLarge) {
		// 413 rather than 400: the request is well-formed, the object it names
		// is simply bigger than this deployment will composite. Same status a
		// too-large source gets, for the same reason.
		s.fail(w, r, http.StatusRequestEntityTooLarge, "SourceTooLarge", ErrWatermarkTooLarge.Error())
		return
	}
	if errors.Is(err, context.DeadlineExceeded) {
		s.fail(w, r, http.StatusGatewayTimeout, "RequestTimeout", "image processing timed out")
		return
	}
	// A failure to get the bytes at all is not a processing failure, and its
	// status comes from the source rather than the pipeline.
	var big errSourceTooLarge
	if errors.As(err, &big) || errors.Is(err, source.ErrNotFound) {
		s.failFetch(w, r, err)
		return
	}
	// The pipeline's own errors carry a status code and a public message;
	// anything else is ours and gets a generic 422.
	var coded interface{ StatusCode() int }
	if errors.As(err, &coded) {
		var pub interface{ PublicMessage() string }
		msg := "the image could not be processed"
		if errors.As(err, &pub) && pub.PublicMessage() != "" {
			msg = pub.PublicMessage()
		}
		s.fail(w, r, coded.StatusCode(), "InvalidImage", msg)
		return
	}
	slog.Error("processing failed", "path", r.URL.Path, "error", err)
	s.fail(w, r, http.StatusUnprocessableEntity, "InvalidImage", "the image could not be processed")
}
