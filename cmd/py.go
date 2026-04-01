package cmd

import (
	"fmt"
	"os"
	"sort"

	"github.com/spf13/cobra"

	"github.com/theapemachine/six/pkg/lang/pysix"
)

var (
	pyVar string
	pyHex bool
)

/*
pyCmd runs python3-exported AST through pkg/lang/pysix and executes stepwise on
one frame; it prints bindings after the last statement.
*/
var pyCmd = &cobra.Command{
	Use:   "py <script.py>",
	Short: "Run a restricted Python script as stepwise program on one frame",
	Long: `Compiles a small Python subset (see pkg/lang/pysix) via a host python3
process, then executes the resulting descriptors with stepwise.RunScalar.

Requires python3 on PATH. Not full Python — only constructs supported by pysix.

Examples:
  six py tools/pysix_demo.py --var s
  six py ./script.py --hex
  six py ./script.py`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {

		path := args[0]
		src, errRead := os.ReadFile(path)

		if errRead != nil {
			return fmt.Errorf("py: read %q: %w", path, errRead)
		}

		prog, locals, errCompile := pysix.CompileSource(string(src))

		if errCompile != nil {
			return fmt.Errorf("py: compile: %w", errCompile)
		}

		var frame [128]uint64

		if errRun := pysix.Run(&frame, prog); errRun != nil {
			return fmt.Errorf("py: run: %w", errRun)
		}

		if pyVar != "" {
			slot, ok := locals[pyVar]

			if !ok {
				return fmt.Errorf("py: no local %q (known: %v)", pyVar, sortedKeys(locals))
			}

			printValue(pyVar, frame[slot], pyHex)

			return nil
		}

		names := sortedKeys(locals)

		for _, name := range names {
			printValue(name, frame[locals[name]], pyHex)
		}

		return nil
	},
}

func sortedKeys(locals map[string]uint8) []string {

	out := make([]string, 0, len(locals))

	for name := range locals {
		out = append(out, name)
	}

	sort.Strings(out)

	return out
}

func printValue(name string, value uint64, asHex bool) {

	if asHex {
		fmt.Printf("%s = %#016x\n", name, value)

		return
	}

	fmt.Printf("%s = %d\n", name, value)
}

func init() {

	pyCmd.Flags().StringVar(&pyVar, "var", "", "print only this local (default: all locals, sorted by name)")
	pyCmd.Flags().BoolVar(&pyHex, "hex", false, "print values in hex")

	rootCmd.AddCommand(pyCmd)
}
