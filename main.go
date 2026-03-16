package main

import (
	"os"

	"github.com/theapemachine/six/cmd"
	"github.com/theapemachine/six/pkg/system/console"
)

func main() {
	if err := console.Error(cmd.Execute()); err != nil {
		os.Exit(1)
	}
}


