// Package source resolves a request path to the bytes of a source object.
//
// Files are read from a directory, with bounded header peeking and whole-file reads.
package source

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
)

// HeaderWindow is the largest prefix Object.Peek can return.
//
// It matches imageinfo.HeaderBytes, which is the only caller that asks for a
// large one. The package does not import imageinfo — the dependency would point
// the wrong way — so httpserver asserts the two agree at compile time.
const HeaderWindow = 64 << 10

var (
	// ErrNotFound means the object does not exist.
	ErrNotFound = errors.New("source: not found")
	// ErrOutsideRoot means the resolved path escaped the root directory.
	ErrOutsideRoot = errors.New("source: path escapes the root directory")
	// ErrNotRegular means the path exists but is a directory, device, socket...
	ErrNotRegular = errors.New("source: not a regular file")
	// ErrTooLarge means the object outran the limit given to ReadAll.
	ErrTooLarge = errors.New("source: object exceeds the read limit")
)

// Source is where source objects come from.
type Source interface {
	// Stat returns an object's identity without reading its body.
	Stat(ctx context.Context, urlPath string) (*Info, error)

	// Open returns the object with its whole body readable.
	Open(ctx context.Context, urlPath string) (*Object, error)

	// Describe names the root directory, for startup logs and nothing else.
	Describe() string
}

// Info identifies an object well enough to build a cache key from it, without
// reading a byte of the body.
type Info struct {
	// Key is the absolute path on disk, never shown to clients.
	Key string
	// Size is the file length in bytes.
	Size int64
	// ModTime is the modification time in nanoseconds.
	ModTime int64
}

// Object is an open source object: its identity plus a readable body.
//
// The body is read forward only. Peek is the exception, and the reason the type
// exists rather than being a plain io.ReadCloser: the header walk needs to look
// at the first 64 KiB and then, sometimes, hand the whole object to libvips
// starting from byte zero. Peek leaves the bytes in the buffer so ReadAll still
// sees them.
type Object struct {
	Info

	br     *bufio.Reader
	closer io.Closer
}

func newObject(info Info, r io.Reader, closer io.Closer) *Object {
	return &Object{
		Info:   info,
		br:     bufio.NewReaderSize(r, HeaderWindow),
		closer: closer,
	}
}

// Peek returns up to n leading bytes without consuming them, so a later Read or
// ReadAll still starts at byte zero. n must not exceed HeaderWindow.
//
// A short object yields a short slice and a nil error: every caller here treats
// "that is the whole file" as an ordinary case, not a failure.
func (o *Object) Peek(n int) ([]byte, error) {
	if n > HeaderWindow {
		return nil, fmt.Errorf("source: peek of %d exceeds the %d-byte window", n, HeaderWindow)
	}
	b, err := o.br.Peek(n)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, bufio.ErrBufferFull) {
		return b, err
	}
	return b, nil
}

func (o *Object) Read(p []byte) (int, error) { return o.br.Read(p) }

// WriteTo streams the remaining body, so passing an object through to a client
// does not buffer it.
func (o *Object) WriteTo(w io.Writer) (int64, error) { return o.br.WriteTo(w) }

// ReadAll reads the whole body into memory, refusing at limit bytes.
//
// The limit reads one byte past the bound and fails if that byte exists,
// including when a file grows after stat. A nonpositive limit means no bound.
func (o *Object) ReadAll(limit int64) ([]byte, error) {
	if limit <= 0 {
		return io.ReadAll(o.br)
	}

	b, err := io.ReadAll(io.LimitReader(o.br, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, ErrTooLarge
	}
	return b, nil
}

func (o *Object) Close() error {
	if o.closer == nil {
		return nil
	}
	return o.closer.Close()
}
