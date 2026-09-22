package menu

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"shipwreck/internal/docker"
	"shipwreck/internal/term"
	"time"
)

type MenuOption[A int, B string] struct {
	Number A
	Value  B
}

const timeout = 10 * time.Second

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

/*
*

	Lists the containers as a picker and kills the one the user lands on. The
	fetch and the kill get a timeout each rather than sharing one, since the
	time spent deciding sits between them and would otherwise eat the budget.

*
*/
func sigkillContainer(in *bufio.Reader) {
	fmt.Print("\nFetching containers...\n")

	containers, err := listForPicker()
	if err != nil {
		fmt.Fprintln(os.Stderr, "shipwreck:", err)
		return
	}

	if len(containers) == 0 {
		fmt.Print("\nno containers\n")
		return
	}

	columns, rows := docker.Rows(containers)

	choices := make([]term.Choice[docker.Container], 0, len(containers))
	for i, c := range containers {
		choices = append(choices, term.Choice[docker.Container]{Label: rows[i], Value: c})
	}

	const hint = "up/down to move, Enter to sigkill, b or q to go back"

	// The column header is indented to line up with the unselected rows, which
	// the picker draws behind three spaces.
	header := "\n" + title + "\n" + hint + "\n\n   " + columns

	picked, err := term.Select(in, header, choices)
	if errors.Is(err, term.ErrBack) || errors.Is(err, term.ErrCancelled) {
		return
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "shipwreck:", err)
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

func listForPicker() ([]docker.Container, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return docker.List(ctx)
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
		{Number: 3, Value: "Abandon Ship! (Exit)"},
	}
}
