package cmd

import (
	"bufio"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"
)

var paperCmd = &cobra.Command{
	Use:   "paper",
	Short: "Run distributed best-fill worker",
	Long:  "Starts a Cap'n Proto-based worker that serves heterogeneous best-fill shard requests.",
	Run: func(cmd *cobra.Command, args []string) {
		paperDir := "paper"
		includeDir := filepath.Join(paperDir, "include")
		manuscriptPath := filepath.Join(paperDir, "manuscript.tex")
		mainTexPath := filepath.Join(paperDir, "main.tex")

		// Read preamble from manuscript.tex
		manuscriptFile, err := os.Open(manuscriptPath)
		if err != nil {
			log.Fatalf("Failed to open manuscript.tex: %v", err)
		}
		defer manuscriptFile.Close()

		var preamble []string
		scanner := bufio.NewScanner(manuscriptFile)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.Contains(line, "\\begin{document}") {
				break
			}
			preamble = append(preamble, line)
		}

		mainFile, err := os.Create(mainTexPath)
		if err != nil {
			log.Fatalf("Failed to create main.tex: %v", err)
		}
		defer mainFile.Close()

		for _, line := range preamble {
			fmt.Fprintln(mainFile, line)
		}
		
		// Add unicode handling for the tests that emit α₁ and α₂
		fmt.Fprintln(mainFile, "\\usepackage{newunicodechar}")
		fmt.Fprintln(mainFile, "\\newunicodechar{₁}{\\ensuremath{_1}}")
		fmt.Fprintln(mainFile, "\\newunicodechar{₂}{\\ensuremath{_2}}")
		fmt.Fprintln(mainFile, "\\newunicodechar{α}{\\ensuremath{\\alpha}}")
		
		fmt.Fprintln(mainFile, "")
		fmt.Fprintln(mainFile, "\\begin{document}")
		
		fmt.Fprintln(mainFile, "")
		fmt.Fprintln(mainFile, "\\maketitle")
		fmt.Fprintln(mainFile, "")

		// Scan includeDir for modules (subdirectories)
		entries, err := os.ReadDir(includeDir)
		if err != nil {
			// If include dir does not exist, just end document
			fmt.Fprintln(mainFile, "\\end{document}")
			return
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			
			moduleName := entry.Name()
			// Capitalize module name for section
			sectionName := strings.ToUpper(moduleName[:1]) + moduleName[1:]
			
			fmt.Fprintf(mainFile, "\\section{%s}\n\n", sectionName)
			fmt.Fprintf(mainFile, "\\graphicspath{{include/%s/}}\n\n", moduleName)

			modulePath := filepath.Join(includeDir, moduleName)
			var texFiles []string
			filepath.WalkDir(modulePath, func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if !d.IsDir() && strings.HasSuffix(d.Name(), ".tex") {
					// relative to paper dir
					relPath, _ := filepath.Rel(paperDir, path)
					texFiles = append(texFiles, relPath)
				}
				return nil
			})

			slices.Sort(texFiles)

			for _, texFile := range texFiles {
				// Convert windows path to unix if needed
				texFile = strings.ReplaceAll(texFile, "\\", "/")
				fmt.Fprintf(mainFile, "\\InputIfFileExists{%s}{}{}\n", texFile)
			}
			
			fmt.Fprintln(mainFile, "\\FloatBarrier")
			fmt.Fprintln(mainFile, "\\clearpage")
			fmt.Fprintln(mainFile, "")
		}

		fmt.Fprintln(mainFile, "\\end{document}")
		fmt.Println("Generated paper/main.tex successfully.")
	},
}

func init() {
	rootCmd.AddCommand(paperCmd)
}
