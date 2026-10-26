//go:build windows

package main

import "os"

func acquireStoreLock(string) (*os.File, error) {
	return nil, storeError(500, "durable alert storage is not supported on Windows")
}
