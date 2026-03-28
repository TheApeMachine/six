package main

import (
	"context"
	"os"

	"github.com/theapemachine/six/cmd"
	"github.com/theapemachine/six/pkg/errnie"
)

func main() {
	err := errnie.Error(cmd.Execute())
	_ = errnie.Shutdown(context.Background())
	if err != nil {
		os.Exit(1)
	}
}
