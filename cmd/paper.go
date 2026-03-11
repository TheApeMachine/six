package cmd

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var paperCmd = &cobra.Command{
	Use:   "paper",
	Short: "Compile LaTeX paper from experiment artifacts",
	Long:  "Aggregates generated chart graphics and LaTeX fragments into a unified main.tex.",
	RunE: func(cmd *cobra.Command, args []string) error {
		paperDir := viper.GetString("paper.dir")
		if paperDir == "" {
			paperDir = "paper"
		}
		includeDir := filepath.Join(paperDir, "include")
		manuscriptPath := filepath.Join(paperDir, "manuscript.tex")
		mainTexPath := filepath.Join(paperDir, "main.tex")
		preamblePath := filepath.Join(paperDir, "preamble.tex")

		// Read preamble from manuscript.tex
		manuscriptFile, err := os.Open(manuscriptPath)
		if err != nil {
			return fmt.Errorf("failed to open manuscript.tex: %w", err)
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
			return fmt.Errorf("failed to create main.tex: %w", err)
		}
		defer mainFile.Close()

		for _, line := range preamble {
			fmt.Fprintln(mainFile, line)
		}

		// Append external preamble file (unicode char defs, etc.) if it exists.
		if preambleBytes, err := os.ReadFile(preamblePath); err == nil {
			fmt.Fprintln(mainFile, "")
			fmt.Fprintln(mainFile, strings.TrimRight(string(preambleBytes), "\n"))
		}

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
			return nil
		}

		// Define document ordering: front-matter first, experiments, back-matter last.
		// "sections" contains abstract/intro/conclusion etc. — split into front and back.
		frontMatterFiles := []string{
			"include/sections/abstract.tex",
			"include/sections/introduction.tex",
		}
		backMatterFiles := []string{
			"include/sections/discussion.tex",
			"include/sections/conclusion.tex",
			"include/sections/related_work.tex",
		}

		// Emit front-matter (no \section wrapper — these files define their own).
		fmt.Fprintln(mainFile, "\\graphicspath{{include/sections/}}")
		fmt.Fprintln(mainFile, "")
		for _, f := range frontMatterFiles {
			fmt.Fprintf(mainFile, "\\InputIfFileExists{%s}{}{}\n", f)
		}

		// Emit remaining sections/ files as theory body (architecture, etc.)
		sectionsDir := filepath.Join(includeDir, "sections")
		if sectionsEntries, err := os.ReadDir(sectionsDir); err == nil {
			skipSet := map[string]bool{}
			for _, f := range frontMatterFiles {
				skipSet[filepath.Base(f)] = true
			}
			for _, f := range backMatterFiles {
				skipSet[filepath.Base(f)] = true
			}
			// experiments.tex is auto-generated index, skip it
			skipSet["experiments.tex"] = true

			var theoryFiles []string
			for _, e := range sectionsEntries {
				if !e.IsDir() && strings.HasSuffix(e.Name(), ".tex") && !skipSet[e.Name()] {
					theoryFiles = append(theoryFiles, e.Name())
				}
			}
			slices.Sort(theoryFiles)
			for _, f := range theoryFiles {
				fmt.Fprintf(mainFile, "\\InputIfFileExists{include/sections/%s}{}{}\n", f)
			}
		}
		fmt.Fprintln(mainFile, "\\FloatBarrier")
		fmt.Fprintln(mainFile, "\\clearpage")
		fmt.Fprintln(mainFile, "")

		// Emit experiment sections in alphabetical order (skip "sections").
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}

			moduleName := entry.Name()
			if moduleName == "sections" {
				continue // already handled above
			}

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

		// Emit back-matter.
		fmt.Fprintln(mainFile, "\\graphicspath{{include/sections/}}")
		for _, f := range backMatterFiles {
			fmt.Fprintf(mainFile, "\\InputIfFileExists{%s}{}{}\n", f)
		}
		fmt.Fprintln(mainFile, "\\FloatBarrier")
		fmt.Fprintln(mainFile, "\\clearpage")

		fmt.Fprintln(mainFile, "\\end{document}")
		fmt.Println("Generated paper/main.tex successfully.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(paperCmd)
}
