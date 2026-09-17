/*
*

	Package term renders an interactive, arrow-key driven picker using only the
	standard library. When stdin cannot be put into raw mode -- input is piped,
	or there is no console -- Select falls back to a numbered prompt.

*
*/
package term

import (
	"bufio"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ErrCancelled is returned when the user backs out with q, Esc or Ctrl+C.
var ErrCancelled = errors.New("cancelled")

// ErrNoChoices is returned when there is nothing to pick from.
var ErrNoChoices = errors.New("nothing to select")

// ErrBack is returned when the user presses b to step back one menu.
var ErrBack = errors.New("back")

const (
	cursorUp   = "\x1b[A"
	clearLine  = "\x1b[2K\r"
	invert     = "\x1b[7m"
	reset      = "\x1b[0m"
	hideCursor = "\x1b[?25l"
	showCursor = "\x1b[?25h"
)

// Choice pairs the value a caller wants back with the line shown for it.
type Choice[T any] struct {
	Label string
	Value T
}

type key int

const (
	keyNone key = iota
	keyUp
	keyDown
	keyEnter
	keyCancel
	keyBack
)

/*
*

	Select draws one line per choice and returns the value the user picks.
	The reader is the caller's stdin reader so buffered bytes are never split
	between two owners. A multi-line header is fine; the repaint counts its
	lines.

*
*/
func Select[T any](in *bufio.Reader, header string, choices []Choice[T]) (T, error) {
	var zero T
	if len(choices) == 0 {
		return zero, ErrNoChoices
	}

	restore, err := makeRaw()
	if err != nil {
		return numbered(in, header, choices)
	}

	// Deferred calls run while a panic unwinds, so the console is restored on
	// that path too. Only os.Exit would skip it.
	defer restore()

	fmt.Print(hideCursor)
	defer fmt.Print(showCursor)

	cursor := 0
	draw(header, choices, cursor, false)

	buf := make([]byte, 16)
	for {
		n, err := in.Read(buf)
		if err != nil || n == 0 {
			return zero, ErrCancelled
		}

		switch decode(buf[:n]) {
		case keyUp:
			if cursor > 0 {
				cursor--
			}
		case keyDown:
			if cursor < len(choices)-1 {
				cursor++
			}
		case keyEnter:
			draw(header, choices, cursor, true)
			return choices[cursor].Value, nil
		case keyCancel:
			draw(header, choices, cursor, true)
			return zero, ErrCancelled
		case keyBack:
			draw(header, choices, cursor, true)
			return zero, ErrBack
		default:
			continue
		}

		draw(header, choices, cursor, true)
	}
}

/*
*

	Repaints the list in place. Every line ends in CR LF because raw mode does
	not translate a bare newline into a carriage return.

*
*/
func draw[T any](header string, choices []Choice[T], cursor int, repaint bool) {
	var b strings.Builder

	headerLines := strings.Split(strings.ReplaceAll(header, "\r\n", "\n"), "\n")

	if repaint {
		// every header line + a blank line + one line per choice
		for range len(headerLines) + len(choices) + 1 {
			b.WriteString(cursorUp)
			b.WriteString(clearLine)
		}
	}

	width := 0
	for _, c := range choices {
		if len(c.Label) > width {
			width = len(c.Label)
		}
	}

	// Anything wider than the terminal wraps onto a second line, which throws
	// off the cursor-up count above and corrupts the next repaint.
	cols := termWidth()

	if limit := cols - 6; limit > 10 && width > limit {
		width = limit
	}

	for _, line := range headerLines {
		if cols > 10 {
			line = truncate(line, cols-1)
		}

		fmt.Fprintf(&b, "%s\r\n", line)
	}

	b.WriteString("\r\n")

	for i, c := range choices {
		label := truncate(c.Label, width)

		if i == cursor {
			fmt.Fprintf(&b, " %s> %-*s %s\r\n", invert, width, label, reset)
			continue
		}

		fmt.Fprintf(&b, "   %-*s \r\n", width, label)
	}

	fmt.Print(b.String())
}

func truncate(s string, width int) string {
	if len(s) <= width {
		return s
	}

	if width <= 3 {
		return s[:width]
	}

	return s[:width-3] + "..."
}

func decode(b []byte) key {
	if len(b) >= 3 && b[0] == 0x1b && b[1] == '[' {
		switch b[2] {
		case 'A':
			return keyUp
		case 'B':
			return keyDown
		}

		return keyNone
	}

	switch b[0] {
	case '\r', '\n':
		return keyEnter

	case 0x1b, 0x03, 'q', 'Q':
		return keyCancel

	case 'b', 'B':
		return keyBack

	case 'k':
		return keyUp

	case 'j':
		return keyDown
	}

	return keyNone
}

/*
*

	Fallback for a non-console stdin: the same list, picked by number.

*
*/
func numbered[T any](in *bufio.Reader, header string, choices []Choice[T]) (T, error) {
	var zero T

	fmt.Printf("%s\n\n", header)
	for i, c := range choices {
		fmt.Printf("  %2d  %s\n", i+1, c.Label)
	}

	fmt.Print("\nSelect a number (b to go back, blank to cancel): ")

	line, err := in.ReadString('\n')
	if err != nil {
		return zero, ErrCancelled
	}

	line = strings.TrimSpace(line)
	if line == "b" || line == "B" {
		return zero, ErrBack
	}

	if line == "" || line == "q" {
		return zero, ErrCancelled
	}

	n, err := strconv.Atoi(line)
	if err != nil || n < 1 || n > len(choices) {
		return zero, fmt.Errorf("invalid selection %q", line)
	}

	return choices[n-1].Value, nil
}

// Interactive reports whether stdin is a console, for callers that want to
// adjust their prompts.
func Interactive() bool {
	restore, err := makeRaw()
	if err != nil {
		return false
	}

	restore()
	return true
}
