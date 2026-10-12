package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vndroid/iXoSS/internal/httpserver"
	"github.com/vndroid/iXoSS/internal/security"
)

func TestSignedMessageMatchesRequest(t *testing.T) {
	cfg := security.NewDefaultConfig()
	cfg.Keys = [][]byte{[]byte("test key")}
	cfg.Salts = [][]byte{[]byte("test salt")}
	checker, err := security.New(&cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{
		"/photo.jpg",
		"//photo.jpg",
		"//sub//photo.jpg?x-oss-process=image/info",
		"///sub///photo.jpg?x-oss-process=image%2Fresize,w_8&v=1",
		"/sub//photo.jpg?x-oss-process=image/format,png",
		"//sub/%E5%9B%BE%20%E7%89%87.jpg?x-oss-process=image/info",
		"https://example.com//sub//photo.jpg?x-oss-process=image/info",
	} {
		t.Run(target, func(t *testing.T) {
			message, err := signedMessage(target)
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest(http.MethodGet, target, nil)
			want := httpserver.SignedMessage(r.URL.Path, r.URL.Query().Get("x-oss-process"))
			sig, err := checker.Sign(message)
			if err != nil {
				t.Fatal(err)
			}
			if err := checker.VerifySignature(context.Background(), sig, want); err != nil {
				t.Fatalf("signed %q, request needs %q: %v", message, want, err)
			}
		})
	}
}

func TestSignedMessageRelativePath(t *testing.T) {
	for _, target := range []string{"photo.jpg?x-oss-process=image/info", ""} {
		got, err := signedMessage(target)
		if err != nil {
			t.Fatal(err)
		}
		want := "/photo.jpg?x-oss-process=image/info"
		if target == "" {
			want = "/"
		}
		if got != want {
			t.Errorf("signedMessage(%q) = %q, want %q", target, got, want)
		}
	}
	if _, err := signedMessage("//sub/%zz.jpg"); err == nil {
		t.Error("invalid path escape was accepted")
	}
}

func TestSignedMessageIgnoresFragment(t *testing.T) {
	for _, target := range []string{
		"//sub//photo.jpg?x-oss-process=image/info#preview",
		"https://example.com//sub//photo.jpg?x-oss-process=image/info#preview",
	} {
		got, err := signedMessage(target)
		if err != nil {
			t.Fatal(err)
		}
		const want = "/sub/photo.jpg?x-oss-process=image/info"
		if got != want {
			t.Errorf("signedMessage(%q) = %q, want %q", target, got, want)
		}
	}
}
