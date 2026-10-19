package storage

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type LocalProof struct{ root string }

func NewLocalProof(root string) *LocalProof { return &LocalProof{root: root} }

var proofName = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}\.(jpg|png|pdf)$`)

func (s *LocalProof) Save(ctx context.Context, data []byte, extension string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if extension != ".jpg" && extension != ".png" && extension != ".pdf" {
		return "", errors.New("unsupported proof extension")
	}
	if err := os.MkdirAll(s.root, 0750); err != nil {
		return "", fmt.Errorf("create proof directory: %w", err)
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", fmt.Errorf("generate proof name: %w", err)
	}
	id[6] = (id[6] & 0x0f) | 0x40
	id[8] = (id[8] & 0x3f) | 0x80
	name := fmt.Sprintf("%x-%x-%x-%x-%x%s", id[:4], id[4:6], id[6:8], id[8:10], id[10:], extension)
	path := filepath.Join(s.root, name)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", fmt.Errorf("create proof file: %w", err)
	}
	// Remove any partial file, including when the request was canceled.
	complete := false
	defer func() {
		if !complete {
			file.Close()
			os.Remove(path)
		}
	}()
	if _, err := file.Write(data); err != nil {
		return "", fmt.Errorf("write proof: %w", err)
	}
	if err := file.Sync(); err != nil {
		return "", fmt.Errorf("sync proof: %w", err)
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("close proof: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	complete = true
	return "uploads/" + name, nil
}

func (s *LocalProof) Delete(path string) error {
	name, ok := strings.CutPrefix(path, "uploads/")
	if !ok || !proofName.MatchString(name) {
		return errors.New("invalid proof cleanup path")
	}
	if err := os.Remove(filepath.Join(s.root, name)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove proof: %w", err)
	}
	return nil
}
