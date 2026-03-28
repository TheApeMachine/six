package main

import (
	"context"
	"os"

	"github.com/theapemachine/six/cmd"
	"github.com/theapemachine/six/pkg/errnie"
)

func main() {
	defer func() { _ = errnie.Shutdown(context.Background()) }()
	if err := errnie.Error(cmd.Execute()); err != nil {
		os.Exit(1)
	}
}
