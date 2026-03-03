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
	"github.com/theapemachine/six/experiment/task"
	"github.com/theapemachine/six/experiment/task/phasedial"
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
	projectName = "six"
	experimentName       string
	cfgFile     string

	rootCmd = &cobra.Command{
		Use:   "six",
		Short: "Check yo six",
		Long:  roottxt,
		Run: func(cmd *cobra.Command, args []string) {
			var exp task.Interface
			
			switch experimentName {
			case "phasedial":
				exp = phasedial.New()
			default:
				log.Fatalf("Unknown experiment: %s", experimentName)
			}

			if err := exp.Run(); err != nil {
				log.Fatalf("Experiment failed: %v", err)
			}
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

	rootCmd.PersistentFlags().StringVar(
		&experimentName, "experiment", "phasedial", "experiment to use",
	)
}

func initConfig() {
	var err error

	if err = writeConfig(); err != nil {
		log.Fatal(err)
	}

	viper.SetConfigName("config")
	viper.SetConfigType("yml")
	viper.AddConfigPath("$HOME/." + projectName)

	if err = viper.ReadInConfig(); err != nil {
		log.Println("failed to read config file", err)
		return
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