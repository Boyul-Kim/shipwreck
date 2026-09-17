package menu

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"shipwreck/internal/docker"
	"shipwreck/internal/term"
)

type MenuOption[A int, B string] struct {
	Number A
	Value  B
}

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
			fmt.Print("\nFetching containers...\n\n")
			if err := docker.Run(); err != nil {
				fmt.Fprintln(os.Stderr, "shipwreck:", err)
			}

		case 2:
			abandonShip()
			return
		}
	}
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
		{Number: 2, Value: "Abandon Ship! (Exit)"},
	}
}
