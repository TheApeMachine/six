package main

import (
	"os"

	"github.com/theapemachine/six/cmd"
	"github.com/theapemachine/six/pkg/errnie"
)

func main() {
	if err := errnie.Error(cmd.Execute()); err != nil {
		os.Exit(1)
	}
}
