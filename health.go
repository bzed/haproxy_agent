package main

import (
	"os"
	"sync"
)

type health struct {
	lastHealth      int32
	lastIdle        int64
	lastTotal       int64
	drainFile       string
	drainFileExists bool
	maintFile       string
	maintFileExists bool
	lock            sync.RWMutex
}

func (h *health) UpdateFileStatus() {
	h.drainFileExists = fileExists(h.drainFile)
	h.maintFileExists = fileExists(h.maintFile)
}

func (h *health) Status() string {
	if h.maintFileExists {
		return "maint"
	}
	if h.drainFileExists {
		return "drain"
	}
	return "ready"
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}
