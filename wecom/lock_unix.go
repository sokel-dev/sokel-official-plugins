//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package main

import (
	"fmt"
	"os"
	"syscall"
)

func acquireStoreLock(path string) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, fmt.Errorf("open store lock: %w", err)
	}
	syscall.CloseOnExec(fd)
	f := os.NewFile(uintptr(fd), path)
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		f.Close()
		return nil, storeError(500, "store lock must be a regular file")
	}
	if err = f.Chmod(0600); err != nil {
		f.Close()
		return nil, err
	}
	if err = syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("store directory is locked by another instance: %w", err)
	}
	return f, nil
}
