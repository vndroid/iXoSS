package httpserver

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"strings"
)

func contentETag(body []byte) string {
	return fmt.Sprintf(`"%x"`, sha256.Sum256(body))
}

// notModified runs only after the selected resource has passed validation.
// An empty etag is used by cold HEAD: existence can satisfy *, but an output
// validator must not be invented without actually producing the representation.
func notModified(w http.ResponseWriter, r *http.Request, etag string) bool {
	if etag != "" {
		w.Header().Set("ETag", etag)
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	if !matchesETag(strings.Join(r.Header.Values("If-None-Match"), ","), etag) {
		return false
	}
	w.Header().Del("Content-Length")
	w.Header().Del("Content-Type")
	w.WriteHeader(http.StatusNotModified)
	return true
}

// Entity tags are quoted opaque strings; commas inside them are not separators.
// GET/HEAD use weak comparison, including when the response has a strong tag.
func matchesETag(value, etag string) bool {
	value = strings.TrimSpace(value)
	if value == "*" {
		return true
	}
	matched := false
	for value != "" {
		value = strings.TrimSpace(value)
		value = strings.TrimPrefix(value, "W/")
		if len(value) < 2 || value[0] != '"' {
			return false
		}
		end := strings.IndexByte(value[1:], '"') + 1
		if end == 0 {
			return false
		}
		for i := 1; i < end; i++ {
			if c := value[i]; c < 0x21 || c == 0x7f {
				return false
			}
		}
		matched = matched || etag != "" && value[:end+1] == strings.TrimPrefix(etag, "W/")
		value = strings.TrimSpace(value[end+1:])
		if value == "" {
			return matched
		}
		if value[0] != ',' {
			return false
		}
		value = strings.TrimSpace(value[1:])
		if value == "" {
			return false
		}
	}
	return false
}
