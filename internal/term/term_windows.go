package term

import (
	"os"
	"syscall"
	"unsafe"
)

const (
	enableProcessedInput   = 0x0001
	enableLineInput        = 0x0002
	enableEchoInput        = 0x0004
	enableVirtualTermInput = 0x0200

	enableVirtualTermOutput = 0x0004
)

var (
	kernel32           = syscall.NewLazyDLL("kernel32.dll")
	procSetConsoleMode = kernel32.NewProc("SetConsoleMode")
)

func setConsoleMode(h syscall.Handle, mode uint32) error {
	if r, _, err := procSetConsoleMode.Call(uintptr(h), uintptr(mode)); r == 0 {
		return err
	}

	return nil
}

/*
*

	Puts the console into raw mode: no line buffering, no echo, and VT input so
	the arrow keys arrive as the same ESC [ A sequences Unix sends. Fails when
	stdin is not a console, which is the caller's cue to fall back.

*
*/
func makeRaw() (func(), error) {
	in := syscall.Handle(os.Stdin.Fd())
	out := syscall.Handle(os.Stdout.Fd())

	var oldIn, oldOut uint32
	if err := syscall.GetConsoleMode(in, &oldIn); err != nil {
		return nil, err
	}

	if err := syscall.GetConsoleMode(out, &oldOut); err != nil {
		return nil, err
	}

	rawIn := oldIn&^(enableLineInput|enableEchoInput|enableProcessedInput) | enableVirtualTermInput
	if err := setConsoleMode(in, rawIn); err != nil {
		return nil, err
	}

	// Needed for the redraw escapes in legacy conhost; Windows Terminal is
	// already fine.
	if err := setConsoleMode(out, oldOut|enableVirtualTermOutput); err != nil {
		setConsoleMode(in, oldIn)
		return nil, err
	}

	return func() {
		setConsoleMode(in, oldIn)
		setConsoleMode(out, oldOut)
	}, nil
}

var procGetConsoleScreenBufferInfo = kernel32.NewProc("GetConsoleScreenBufferInfo")

type coord struct{ X, Y int16 }

type smallRect struct{ Left, Top, Right, Bottom int16 }

type consoleScreenBufferInfo struct {
	Size              coord
	CursorPosition    coord
	Attributes        uint16
	Window            smallRect
	MaximumWindowSize coord
}

// termWidth is the console width in columns, or 0 when it cannot be read.
func termWidth() int {
	var info consoleScreenBufferInfo

	r, _, _ := procGetConsoleScreenBufferInfo.Call(os.Stdout.Fd(), uintptr(unsafe.Pointer(&info)))
	if r == 0 {
		return 0
	}

	return int(info.Window.Right-info.Window.Left) + 1
}
