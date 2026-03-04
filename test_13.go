package main

import (
	"fmt"

	"github.com/theapemachine/six/experiment/task/codegen"
)
func main() {
	fmt.Println("Running Pipeline with Eigenmode GPU Steering...")
	pipeline := codegen.NewPipeline()
	pipeline.Run()
}
