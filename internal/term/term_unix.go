//go:build unix

package term

import (
	"os"
	"syscall"
	"unsafe"
)

func ioctl(fd uintptr, req uintptr, t *syscall.Termios) error {
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(unsafe.Pointer(t))); errno != 0 {
		return errno
	}

	return nil
}

/*
*

	Puts the terminal into raw mode: no canonical line editing, no echo, and no
	signal generation, so arrow keys and Ctrl+C arrive as plain bytes. Fails
	when stdin is not a tty, which is the caller's cue to fall back.

*
*/
func makeRaw() (func(), error) {
	fd := os.Stdin.Fd()

	var old syscall.Termios
	if err := ioctl(fd, getAttr, &old); err != nil {
		return nil, err
	}

	raw := old
	raw.Lflag &^= syscall.ECHO | syscall.ICANON | syscall.ISIG
	raw.Iflag &^= syscall.IXON | syscall.ICRNL
	raw.Cc[syscall.VMIN] = 1
	raw.Cc[syscall.VTIME] = 0

	if err := ioctl(fd, setAttr, &raw); err != nil {
		return nil, err
	}

	return func() { ioctl(fd, setAttr, &old) }, nil
}

type winsize struct{ Row, Col, Xpixel, Ypixel uint16 }

// termWidth is the terminal width in columns, or 0 when it cannot be read.
func termWidth() int {
	var ws winsize

	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, os.Stdout.Fd(), syscall.TIOCGWINSZ, uintptr(unsafe.Pointer(&ws))); errno != 0 {
		return 0
	}

	return int(ws.Col)
}
