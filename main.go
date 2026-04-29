package main

import (
	"fmt"
	"os"

	"github.com/natefinch/skink/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "skink:", err)
		os.Exit(1)
	}
}
