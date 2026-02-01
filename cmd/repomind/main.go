package main

import (
	"os"

	"github.com/repomind/repomind-go/internal/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
