package httpserver

import (
	"testing"

	"github.com/vndroid/iXoSS/internal/auximageprovider"
	"github.com/vndroid/iXoSS/internal/processing"
	"github.com/vndroid/iXoSS/internal/source"
)

func TestNewRequiresRoot(t *testing.T) {
	called := false
	_, err := New(Config{}, func(auximageprovider.Provider) (*processing.Processor, error) {
		called = true
		return nil, nil
	})
	if err == nil {
		t.Fatal("New without Root must fail rather than serve the working directory")
	}
	if called {
		t.Error("processor initialized before rejecting the missing root")
	}
}

func TestLocalCacheKeyIsUnchanged(t *testing.T) {
	s := newLimitedServer(t, t.TempDir(), 1024)
	info := &source.Info{Key: "/images/x.jpg", Size: 10, ModTime: 1700000000}
	got := s.cacheKey(info, "image/format,png")
	// Pin the digest produced by the previous local implementation, so removing
	// remote validators cannot silently invalidate an existing disk cache.
	const legacyHash = "4e4bdc167588b635704a41b84fe2df8b3c5b94b5fb033333058571da7e943f4f"
	if got.Hash() != legacyHash {
		t.Fatalf("local cache hash = %s, want %s", got.Hash(), legacyHash)
	}
	info.ModTime++
	if s.cacheKey(info, got.Chain).Hash() == got.Hash() {
		t.Error("a changed modification time must invalidate a cached transform")
	}
}
