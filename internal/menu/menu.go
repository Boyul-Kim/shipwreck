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

		ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
		defer cancel()

		switch picked {
		case 1:
			fmt.Print("\nFetching containers...\n\n")

			out, err := docker.List(ctx)
			if err != nil {
				fmt.Fprintln(os.Stderr, "shipwreck:", err)
			}

			docker.Render(out)
		case 2:
			fmt.Print("\nSigkill for container...\n\n")
			id := "159ca8962ba64d79a7866f2c5de18c12a8d5e22667290986edec94482368acf1"
			if err := docker.Sigkill(ctx, id); err != nil {
				fmt.Fprintln(os.Stderr, "shipwreck:", err)
			}

			fmt.Print("\nSigkill successful")
		case 3:
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
		{Number: 2, Value: "Sigkill Container"},
		{Number: 3, Value: "Abandon Ship! (Exit)"},
	}
}
