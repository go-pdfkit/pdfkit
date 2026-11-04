// Copyright (c) 2026 the go-pdfkit/pdfkit authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

//go:build !unix

package pdfkit

import "errors"

// capAddressSpace has no counterpart outside the Unixes, so the bomb test
// skips rather than running without the limit that makes it safe.
func capAddressSpace(uint64) error {
	return errors.New("no RLIMIT_AS on this platform")
}
