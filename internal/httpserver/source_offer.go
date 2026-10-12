package httpserver

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// SourceArchivePath is reserved only when an operator configures SourceArchive.
const SourceArchivePath = "/_ixoss/source.tar.gz"

func validateSourceOffer(cfg Config) error {
	if cfg.SourceURL != "" {
		u, err := url.Parse(cfg.SourceURL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" ||
			u.User != nil || u.Fragment != "" || strings.ContainsAny(cfg.SourceURL, "\r\n<>\"\\") {
			return fmt.Errorf("IXOSS_SOURCE_URL must be an absolute HTTP(S) URL without credentials or fragment")
		}
	}
	if cfg.SourceArchive != "" {
		f, err := os.Open(cfg.SourceArchive)
		if err != nil {
			return fmt.Errorf("open IXOSS_SOURCE_ARCHIVE: %w", err)
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("IXOSS_SOURCE_ARCHIVE must be a readable regular file")
		}
	}
	return nil
}

func (s *Server) setSourceOffer(w http.ResponseWriter) {
	link := s.cfg.SourceURL
	if s.cfg.SourceArchive != "" {
		link = SourceArchivePath
	}
	if link != "" {
		w.Header().Add("Link", "<"+link+">; rel=\"describedby\"; title=\"iXoSS corresponding source (AGPL-3.0)\"")
	}
}

func (s *Server) serveSourceArchive(w http.ResponseWriter, r *http.Request) {
	f, err := os.Open(s.cfg.SourceArchive)
	if err != nil {
		s.fail(w, r, http.StatusInternalServerError, "SourceUnavailable", "corresponding source is unavailable")
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		s.fail(w, r, http.StatusInternalServerError, "SourceUnavailable", "corresponding source is unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", "attachment; filename=ixoss-source.tar.gz")
	http.ServeContent(w, r, "ixoss-source.tar.gz", info.ModTime(), f)
}
