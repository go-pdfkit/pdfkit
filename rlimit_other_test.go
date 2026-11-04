// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

//go:build !linux

package pdfkit

import "errors"

// capAddressSpace has no dependable counterpart off Linux — macOS rejects
// RLIMIT_AS with EINVAL — so the bomb test skips rather than running without
// the limit that makes it safe to run at all.
func capAddressSpace(uint64) error {
	return errors.New("no dependable RLIMIT_AS on this platform")
}
