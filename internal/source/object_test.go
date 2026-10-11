package source

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPeekLeavesTheBytesForReadAll(t *testing.T) {
	const body = "0123456789abcdef"
	obj := newObject(Info{Size: int64(len(body))}, strings.NewReader(body), nil)
	head, err := obj.Peek(4)
	if err != nil || string(head) != body[:4] {
		t.Fatalf("Peek = %q, %v; want %q", head, err, body[:4])
	}
	b, err := obj.ReadAll(int64(len(body)))
	if err != nil || string(b) != body {
		t.Fatalf("ReadAll after Peek = %q, %v; want the whole file", b, err)
	}
}

func TestReadAllRefusesPastTheLimit(t *testing.T) {
	obj := newObject(Info{Size: 16}, strings.NewReader("0123456789abcdef"), nil)
	if _, err := obj.ReadAll(8); !errors.Is(err, ErrTooLarge) {
		t.Errorf("got %v, want ErrTooLarge", err)
	}
}

func TestReadAllBoundsAFileThatGrowsAfterStat(t *testing.T) {
	root := t.TempDir()
	name := filepath.Join(root, "growing.bin")
	if err := os.WriteFile(name, []byte("small"), 0600); err != nil {
		t.Fatal(err)
	}
	l, err := NewLocal(root)
	if err != nil {
		t.Fatal(err)
	}
	obj, err := l.Open(context.Background(), "/growing.bin")
	if err != nil {
		t.Fatal(err)
	}
	defer obj.Close()
	if obj.Size != 5 {
		t.Fatalf("stat size = %d, want 5", obj.Size)
	}
	if err := os.WriteFile(name, []byte(strings.Repeat("x", 100)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := obj.ReadAll(10); !errors.Is(err, ErrTooLarge) {
		t.Errorf("a stale stat bypassed the read bound: %v", err)
	}
}

func TestLocalStatAndOpenUseFileIdentity(t *testing.T) {
	root, _ := newRoot(t)
	l, err := NewLocal(root)
	if err != nil {
		t.Fatal(err)
	}
	stat, err := l.Stat(context.Background(), "/ok.txt")
	if err != nil {
		t.Fatal(err)
	}
	obj, err := l.Open(context.Background(), "/ok.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer obj.Close()
	file, err := os.Stat(filepath.Join(root, "ok.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if obj.Info != *stat || stat.ModTime != file.ModTime().UnixNano() || stat.Size != file.Size() {
		t.Fatalf("inconsistent local file identity: stat=%+v, open=%+v", stat, obj.Info)
	}
}
