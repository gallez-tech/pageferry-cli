package main

import (
	"fmt"
	"os"
)

const version = "dev"

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Printf("pageferry %s\n", version)
		return
	}

	fmt.Fprintln(os.Stderr, "pageferry: command not implemented; use --version")
	os.Exit(2)
}
