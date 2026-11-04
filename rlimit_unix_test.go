// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

//go:build unix

package pdfkit

import "syscall"

// capAddressSpace lowers this process's address-space ceiling to n bytes, so a
// decode that asks for more dies here instead of on the machine running the
// tests. It never raises the ceiling: a limit already tighter than n is left
// alone, because something else set it for a reason.
func capAddressSpace(n uint64) error {
	var cur syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_AS, &cur); err != nil {
		return err
	}
	if cur.Cur != 0 && cur.Cur < n {
		return nil
	}
	lim := syscall.Rlimit{Cur: n, Max: cur.Max}
	if cur.Max != 0 && cur.Max < n {
		lim.Cur = cur.Max
	}
	return syscall.Setrlimit(syscall.RLIMIT_AS, &lim)
}
