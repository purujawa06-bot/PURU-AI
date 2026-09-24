//go:build linux

package ai

import (
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// startMemWatch polls the resident memory (RSS) of the whole process group
// every 200ms and SIGKILLs the group when it exceeds budgetBytes. This is a
// hard cap on ACTUAL RAM (unlike ulimit -v, which also counts virtual
// address space and would break Go/Java toolchains that reserve GBs of VSZ
// while using MBs of RAM). No privileges needed, stdlib only.
func startMemWatch(pgid int, budgetBytes int64, stop <-chan struct{}, killed *atomic.Bool) {
	go func() {
		t := time.NewTicker(200 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				if groupRSS(pgid) > budgetBytes {
					killed.Store(true)
					// TERM-first so the shell reaps its children instead
					// of orphaning them under PID 1; the timer escalates
					// to SIGKILL when the group ignores SIGTERM.
					terminateGroup(pgid)
					return
				}
			}
		}
	}()
}

// groupRSS sums RSS of every process in pgid by scanning /proc.
func groupRSS(pgid int) int64 {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}
	var total int64
	for _, e := range entries {
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue // bukan PID
		}
		b, err := os.ReadFile("/proc/" + e.Name() + "/stat")
		if err != nil {
			continue // proses sudah mati / tak terlihat
		}
		g, rss, ok := parseProcStat(b)
		if ok && g == pgid {
			total += rss
		}
	}
	return total
}

// parseProcStat extracts (pgrp, rssBytes) from /proc/<pid>/stat.
// comm may contain spaces/parens, so split after the LAST ')'.
func parseProcStat(b []byte) (pgrp int, rssBytes int64, ok bool) {
	s := string(b)
	i := strings.LastIndexByte(s, ')')
	if i < 0 {
		return 0, 0, false
	}
	f := strings.Fields(s[i+1:])
	// after comm: state(0) ppid(1) pgrp(2) ... rss_pages(21)
	if len(f) < 22 {
		return 0, 0, false
	}
	g, err := strconv.Atoi(f[2])
	if err != nil {
		return 0, 0, false
	}
	pages, err := strconv.ParseInt(f[21], 10, 64)
	if err != nil || pages < 0 {
		return 0, 0, false
	}
	return g, pages * int64(os.Getpagesize()), true
}
