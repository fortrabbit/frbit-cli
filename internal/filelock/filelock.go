// Package filelock serialises work across frbit processes through an exclusive lock on a file.
package filelock

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const pollInterval = 25 * time.Millisecond

type Lock struct {
	file *os.File
}

// Acquire blocks until it holds the lock on path or ctx ends.
func Acquire(ctx context.Context, path string) (*Lock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create lock directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}
	for {
		locked, err := tryLock(file)
		if err != nil {
			_ = file.Close()
			return nil, fmt.Errorf("lock %s: %w", path, err)
		}
		if locked {
			return &Lock{file: file}, nil
		}
		select {
		case <-ctx.Done():
			_ = file.Close()
			return nil, fmt.Errorf("wait for lock %s: %w", path, ctx.Err())
		case <-time.After(pollInterval):
		}
	}
}

func (l *Lock) Release() error {
	unlockErr := unlock(l.file)
	closeErr := l.file.Close()
	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}
