package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize default configuration",
	Long:  initLong,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := writeConfig(); err != nil {
			return fmt.Errorf("%s: %w", ErrConfigInitFailed, err)
		}
		fmt.Println("Configuration successfully initialized.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(initCmd)
}

type InitError string

func (e InitError) Error() string {
	return string(e)
}

const (
	ErrConfigInitFailed InitError = "failed to initialize configuration"
)

const initLong = `
Initialize the default configuration file ($HOME/.six/config.yml).

This command creates the default configuration skeleton in the user's
home directory. If the file already exists, the command will refuse to
overwrite it — remove it manually first or use a different path.

Examples:
	six init
`

func writeConfig() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	configDir := filepath.Join(home, "."+projectName)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return err
	}

	configPath := filepath.Join(configDir, "config.yml")

	// Safety guard: refuse to silently overwrite an existing config.
	// Differentiate "exists" from real I/O errors (permissions, etc.).
	if _, err := os.Stat(configPath); err == nil {
		return fmt.Errorf("configuration already exists at %s; remove it manually to re-initialize", configPath)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("cannot stat %s: %w", configPath, err)
	}

	b, err := embedded.ReadFile("cfg/config.yml")
	if err != nil {
		return err
	}

	fmt.Printf("Writing default configuration to %s\n", configPath)
	return os.WriteFile(configPath, b, 0644)
}

