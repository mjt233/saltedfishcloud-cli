package main

import (
	"os"

	"github.com/mjt233/saltedfishcloud-cli/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
