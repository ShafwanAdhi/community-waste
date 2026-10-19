package storage

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestLocalProofSaveAndDelete(t *testing.T) {
	root := t.TempDir()
	store := NewLocalProof(root)
	for _, extension := range []string{".jpg", ".png", ".pdf"} {
		data := []byte("proof content")
		path, err := store.Save(t.Context(), data, extension)
		if err != nil {
			t.Fatal(err)
		}
		if !regexp.MustCompile(`^uploads/[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}\.(jpg|png|pdf)$`).MatchString(path) {
			t.Fatalf("path: %s", path)
		}
		actual := filepath.Join(root, filepath.Base(path))
		got, err := os.ReadFile(actual)
		if err != nil || !bytes.Equal(got, data) {
			t.Fatalf("stored: %s %v", got, err)
		}
		// A fresh adapter sees the same persisted file.
		if err := NewLocalProof(root).Delete(path); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(actual); !os.IsNotExist(err) {
			t.Fatalf("not deleted: %v", err)
		}
	}
}

func TestLocalProofFailureAndPathBoundaries(t *testing.T) {
	root := t.TempDir()
	blocked := filepath.Join(root, "file")
	if err := os.WriteFile(blocked, []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewLocalProof(blocked).Save(t.Context(), []byte("proof"), ".pdf"); err == nil {
		t.Fatal("expected directory failure")
	}
	store := NewLocalProof(root)
	for _, path := range []string{"../outside", "uploads/../outside", "uploads/a/b.pdf", "uploads/..\\outside", "/absolute.pdf", "uploads/not-a-uuid.pdf"} {
		if err := store.Delete(path); err == nil {
			t.Errorf("accepted cleanup path %q", path)
		}
	}
	if _, err := store.Save(t.Context(), []byte("proof"), ".exe"); err == nil {
		t.Fatal("accepted extension")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := store.Save(ctx, []byte("proof"), ".pdf"); err == nil {
		t.Fatal("accepted canceled context")
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 {
		t.Fatalf("partial files left: %v %v", entries, err)
	}
}
