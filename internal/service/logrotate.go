package service

import (
	"os"
	"path/filepath"
	"sync"
)

type RotatingWriter struct {
	mu      sync.Mutex
	file    *os.File
	path    string
	maxSize int64
	curSize int64
}

func NewRotatingWriter(path string, maxSize int64) (*RotatingWriter, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return &RotatingWriter{file: f, path: path, maxSize: maxSize, curSize: info.Size()}, nil
}

func (w *RotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return 0, os.ErrClosed
	}
	n, err := w.file.Write(p)
	w.curSize += int64(n)
	if w.maxSize > 0 && w.curSize > w.maxSize {
		w.rotate()
	}
	return n, err
}

func (w *RotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	return w.file.Close()
}

func (w *RotatingWriter) rotate() {
	_ = w.file.Close()
	backup := w.path + ".1"
	_ = os.Remove(backup)
	_ = os.Rename(w.path, backup)
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		w.file = nil
		w.curSize = 0
		return
	}
	w.file = f
	w.curSize = 0
}
