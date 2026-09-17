//go:build darwin

package term

import "syscall"

const (
	getAttr = syscall.TIOCGETA
	setAttr = syscall.TIOCSETA
)
