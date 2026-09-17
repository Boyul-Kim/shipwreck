package menu

import (
	"bufio"
	"fmt"
	"os"
	"shipwreck/internal/docker"
	"strings"
)

type MenuOption[A int, B string] struct {
	Number A
	Value  B
}

func Menu() {
	fmt.Println(banner)
	reader := bufio.NewReader(os.Stdin)
	menuOptions := loadMenuOptions()

	for {
		fmt.Println(title)
		for _, option := range menuOptions {
			fmt.Printf("%v: %v\n", option.Number, option.Value)
		}
		fmt.Println("~^~^~^~^~^~^~^~^~^~^~^~^~^~^~^~^~^~^~^~^~\n")

		input, err := reader.ReadString('\n')
		if err != nil {
			fmt.Println("Error reading input:", err)
			continue
		}

		input = strings.TrimSpace(input)

		switch input {
		case "1":
			fmt.Println("\nFetching containers...\n")
			if err := docker.Run(); err != nil {
				fmt.Fprintln(os.Stderr, "shipwreck:", err)
			}
		case "2":
			fmt.Println("\nPaddling away!")
			fmt.Println(exit)
			return
		default:
			fmt.Println("\n[!]Invalid choice")
		}
	}
}

func loadMenuOptions() []MenuOption[int, string] {
	return []MenuOption[int, string]{
		{Number: 1, Value: "Get Containers"},
		{Number: 2, Value: "Abandon Ship! (Exit)"},
	}
}
