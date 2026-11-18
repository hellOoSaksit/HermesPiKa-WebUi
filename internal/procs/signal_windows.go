// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

//go:build windows

package procs

import "os"

var sigterm os.Signal = os.Kill

func alive(int) bool { return false }
