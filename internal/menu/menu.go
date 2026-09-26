package menu

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"shipwreck/internal/docker"
	"shipwreck/internal/term"
	"strconv"
	"strings"
	"time"
)

type MenuOption[A int, B string] struct {
	Number A
	Value  B
}

const timeout = 10 * time.Second

const defaultDrainTimeout = 10

func Menu() {
	fmt.Print(banner)

	reader := bufio.NewReader(os.Stdin)
	choices := loadMenuChoices()

	const hint = "up/down to move, Enter to select, q to abandon ship"

	for {
		picked, err := term.Select(reader, "\n"+title+"\n"+hint, choices)
		if errors.Is(err, term.ErrBack) {
			continue
		}

		if errors.Is(err, term.ErrCancelled) {
			abandonShip()
			return
		}

		if err != nil {
			fmt.Fprintln(os.Stderr, "shipwreck:", err)
			continue
		}

		switch picked {
		case 1:
			listContainers()
		case 2:
			sigkillContainer(reader)
		case 3:
			sigtermContainer(reader)
		case 4:
			stopContainer(reader)
		case 5:
			restartContainer(reader)
		case 6:
			abandonShip()
			return
		}
	}
}

func listContainers() {
	fmt.Print("\nFetching containers...\n\n")

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	containers, err := docker.List(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "shipwreck:", err)
		return
	}

	if err := docker.Render(containers); err != nil {
		fmt.Fprintln(os.Stderr, "shipwreck:", err)
	}
}

func sigkillContainer(in *bufio.Reader) {
	const hint = "up/down to move, Enter to sigkill, b or q to go back"

	picked, ok := pickContainer(in, hint)
	if !ok {
		return
	}

	fmt.Printf("\nSigkill for %s...\n", describe(picked))

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := docker.Sigkill(ctx, picked.ID); err != nil {
		fmt.Fprintln(os.Stderr, "shipwreck:", err)
		return
	}

	fmt.Print("\nSigkill successful\n")
}

func sigtermContainer(in *bufio.Reader) {
	const hint = "up/down to move, Enter to send SIGTERM, b or q to go back"

	picked, ok := pickContainer(in, hint)
	if !ok {
		return
	}

	fmt.Printf("\nSIGTERM for %s...\n", describe(picked))

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := docker.Sigterm(ctx, picked.ID); err != nil {
		fmt.Fprintln(os.Stderr, "shipwreck:", err)
		return
	}

	fmt.Print("\nSIGTERM sent\n")
}

func stopContainer(in *bufio.Reader) {
	const hint = "up/down to move, Enter to stop, b or q to go back"

	picked, ok := pickContainer(in, hint)
	if !ok {
		return
	}

	t, err := promptTimeout(in, defaultDrainTimeout)
	if err != nil {
		fmt.Fprintln(os.Stderr, "shipwreck:", err)
		return
	}

	fmt.Printf("\nStopping %s (draining up to %ds)...\n", describe(picked), t)

	ctx, cancel := context.WithTimeout(context.Background(), timeout+time.Duration(t)*time.Second)
	defer cancel()

	if err := docker.Stop(ctx, picked.ID, t); err != nil {
		fmt.Fprintln(os.Stderr, "shipwreck:", err)
		return
	}

	fmt.Print("\nStop successful\n")
}

func restartContainer(in *bufio.Reader) {
	const hint = "up/down to move, Enter to restart, b or q to go back"

	picked, ok := pickContainer(in, hint)
	if !ok {
		return
	}

	fmt.Printf("\nRestarting %s...\n", describe(picked))

	ctx, cancel := context.WithTimeout(context.Background(), timeout+defaultDrainTimeout*time.Second)
	defer cancel()

	if err := docker.Restart(ctx, picked.ID, defaultDrainTimeout); err != nil {
		fmt.Fprintln(os.Stderr, "shipwreck:", err)
		return
	}

	fmt.Print("\nRestart successful\n")
}

func pickContainer(in *bufio.Reader, hint string) (docker.Container, bool) {
	fmt.Print("\nFetching containers...\n")

	containers, err := listForPicker()
	if err != nil {
		fmt.Fprintln(os.Stderr, "shipwreck:", err)
		return docker.Container{}, false
	}

	if len(containers) == 0 {
		fmt.Print("\nno containers\n")
		return docker.Container{}, false
	}

	columns, rows := docker.Rows(containers)

	choices := make([]term.Choice[docker.Container], 0, len(containers))
	for i, c := range containers {
		choices = append(choices, term.Choice[docker.Container]{Label: rows[i], Value: c})
	}

	header := "\n" + title + "\n" + hint + "\n\n   " + columns

	picked, err := term.Select(in, header, choices)
	if errors.Is(err, term.ErrBack) || errors.Is(err, term.ErrCancelled) {
		return docker.Container{}, false
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "shipwreck:", err)
		return docker.Container{}, false
	}

	return picked, true
}

func listForPicker() ([]docker.Container, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return docker.List(ctx)
}

func promptTimeout(in *bufio.Reader, def int) (int, error) {
	fmt.Printf("\nDrain timeout in seconds (blank for %ds): ", def)

	line, err := in.ReadString('\n')
	if err != nil {
		return 0, fmt.Errorf("reading timeout: %w", err)
	}

	line = strings.TrimSpace(line)
	if line == "" {
		return def, nil
	}

	t, err := strconv.Atoi(line)
	if err != nil || t < 0 {
		return 0, fmt.Errorf("invalid timeout %q", line)
	}

	return t, nil
}

func describe(c docker.Container) string {
	if name := c.Name(); name != "" {
		return name
	}

	return c.ShortID()
}

func abandonShip() {
	fmt.Println("\nPaddling away!")
	fmt.Print(exit)
}

func loadMenuChoices() []term.Choice[int] {
	options := loadMenuOptions()

	choices := make([]term.Choice[int], 0, len(options))
	for _, o := range options {
		choices = append(choices, term.Choice[int]{Label: string(o.Value), Value: int(o.Number)})
	}

	return choices
}

func loadMenuOptions() []MenuOption[int, string] {
	return []MenuOption[int, string]{
		{Number: 1, Value: "Get Containers"},
		{Number: 2, Value: "Sigkill Container"},
		{Number: 3, Value: "Sigterm Container"},
		{Number: 4, Value: "Stop Container (drain timeout)"},
		{Number: 5, Value: "Restart Container"},
		{Number: 6, Value: "Abandon Ship! (Exit)"},
	}
}
