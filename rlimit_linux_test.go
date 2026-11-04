// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

//go:build linux

package pdfkit

import (
	"fmt"
	"os"
	"strconv"
	"syscall"
)

// capAddressSpace lets this process reserve headroom more bytes of address
// space than it already has, and no more.
//
// ⛔ The ceiling is RELATIVE, and the first version of this was not. A Go
// program reserves a great deal of address space at startup — arena hints and
// the page allocator's summaries — so an absolute cap of 1 GiB did not bound
// the bomb, it killed the process while it was still encoding a ONE-PIXEL
// fixture. The failure looked exactly like the one the test is for: "fatal
// error: runtime: out of memory", from a test about running out of memory.
//
// What a test can bound is what the program asks for FROM HERE, so that is
// what this bounds.
func capAddressSpace(headroom uint64) error {
	size, err := virtualSize()
	if err != nil {
		return err
	}
	var cur syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_AS, &cur); err != nil {
		return err
	}
	want := size + headroom
	if cur.Cur != ^uint64(0) && cur.Cur < want {
		return nil // something tighter is already in force; leave it be
	}
	lim := syscall.Rlimit{Cur: want, Max: cur.Max}
	if cur.Max != ^uint64(0) && cur.Max < want {
		lim.Cur = cur.Max
	}
	return syscall.Setrlimit(syscall.RLIMIT_AS, &lim)
}

// virtualSize is this process's current address-space size in bytes, the first
// field of /proc/self/statm counted in pages.
func virtualSize() (uint64, error) {
	b, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return 0, err
	}
	field := string(b)
	for i := range len(field) {
		if field[i] == ' ' {
			field = field[:i]
			break
		}
	}
	pages, err := strconv.ParseUint(field, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("/proc/self/statm began %q: %w", field, err)
	}
	return pages * uint64(os.Getpagesize()), nil
}
