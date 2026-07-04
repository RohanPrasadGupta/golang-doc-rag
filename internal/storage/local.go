package storage

import (
	"io"
	"os"
	"path/filepath"
)

type LocalStorage struct {
	basePath string // the folder where files get saved, e.g. "./uploads"
}

func NewLocalStorage(basePath string) *LocalStorage {
	return &LocalStorage{basePath: basePath}

}

func (l *LocalStorage) Save(id string, data io.Reader) (string, error) {

	if err := os.MkdirAll(l.basePath, 0o755); err != nil {
		return "", err
	}

	path := filepath.Join(l.basePath, id)
	out, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer out.Close()
	_, err = io.Copy(out, data)
	if err != nil {
		return "", err
	}
	return path, nil
}
