package menu

import (
	"bufio"
	"fmt"
	"os"
	"shipwreck/internal/docker"
	"strings"
)

func Menu() {
	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Println("\n=== Shipwreck ===")
		fmt.Println("1. Get containers")
		fmt.Println("2. Exit")
		fmt.Println("=================\n")

		input, err := reader.ReadString('\n')
		if err != nil {
			fmt.Println("Error reading input:", err)
			continue
		}

		input = strings.TrimSpace(input)

		switch input {
		case "1":
			fmt.Println("\nFetching containers...")
			if err := docker.Run(); err != nil {
				fmt.Fprintln(os.Stderr, "shipwreck:", err)
			}
		case "2":
			fmt.Println("\nExiting")
			return
		default:
			fmt.Println("\n[!]Invalid choice")
		}
	}
}
