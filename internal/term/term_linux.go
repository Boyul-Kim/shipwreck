//go:build linux

package term

import "syscall"

const (
	getAttr = syscall.TCGETS
	setAttr = syscall.TCSETS
)
