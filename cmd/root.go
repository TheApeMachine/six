package cmd

import (
	"bytes"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/theapemachine/six/utils"
)

/*
Embed a mini filesystem into the binary to hold the default config file.
This will be written to the home directory of the user running the service,
which allows a developer to easily override the config file.
*/
//go:embed cfg/*
var embedded embed.FS

var (
	projectName    = "six"
	cfgFile        string

	rootCmd = &cobra.Command{
		Use:   "six",
		Short: "Check yo six",
		Long:  roottxt,
		Run: func(cmd *cobra.Command, args []string) {
		},
	}
)

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVar(
		&cfgFile, "config", "config.yml", "config file (default is $HOME/."+projectName+"/config.yml)",
	)
}

func initConfig() {
	viper.SetConfigName("config")
	viper.SetConfigType("yml")
	viper.AddConfigPath("$HOME/." + projectName)

	if err := viper.ReadInConfig(); err != nil {
		// Just log loosely since we now optionally rely on embedded config/defaults
		log.Println("Note: No local config file found, using defaults.", err)
	}
}

func writeConfig() (err error) {
	var (
		home, _ = os.UserHomeDir()
		fh      fs.File
		buf     bytes.Buffer
	)

	fullPath := home + "/." + projectName + "/" + cfgFile

	if utils.CheckFileExists(fullPath) {
		return
	}

	if fh, err = embedded.Open("cfg/" + cfgFile); err != nil {
		return fmt.Errorf("failed to open embedded config file: %w", err)
	}

	defer fh.Close()

	if _, err = io.Copy(&buf, fh); err != nil {
		return fmt.Errorf("failed to read embedded config file: %w", err)
	}

	if err = os.Mkdir(home+"/."+projectName, os.ModePerm); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	if err = os.WriteFile(fullPath, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return
}

const roottxt = `
six v0.0.1
`
