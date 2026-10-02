package main

import (
	"os"

	"github.com/moooyo/goby/internal/commanddomain"
)

func main() { os.Exit(commanddomain.RunLauncher()) }
