package pysix

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
)

//go:embed scripts/astexport.py
var astExportScript string

var (
	scriptPathOnce sync.Once
	scriptPath     string
	scriptPathErr  error
)

func astExporterPath() (string, error) {

	scriptPathOnce.Do(func() {
		f, errTmp := os.CreateTemp("", "six-pysix-astexport-*.py")

		if errTmp != nil {
			scriptPathErr = errTmp

			return
		}

		defer f.Close()

		if _, errWrite := f.WriteString(astExportScript); errWrite != nil {
			scriptPathErr = errWrite

			return
		}

		scriptPath = f.Name()
	})

	return scriptPath, scriptPathErr
}

/*
Parse runs python3 on astexport.py reading pythonSource from stdin and returns
the JSON module tree as generic maps.
*/
func Parse(pythonSource string) (map[string]interface{}, error) {

	path, errPath := astExporterPath()

	if errPath != nil {
		return nil, errPath
	}

	cmd := exec.Command("python3", path)
	cmd.Stdin = bytes.NewReader([]byte(pythonSource))

	out, errRun := cmd.Output()

	if errRun != nil {
		var ee *exec.ExitError

		if errors.As(errRun, &ee) && len(ee.Stderr) > 0 {
			return nil, fmt.Errorf("pysix.Parse: python: %w: %s", errRun, ee.Stderr)
		}

		return nil, fmt.Errorf("pysix.Parse: %w", errRun)
	}

	dec := json.NewDecoder(bytes.NewReader(out))
	dec.UseNumber()

	var root map[string]interface{}

	if err := dec.Decode(&root); err != nil {
		return nil, fmt.Errorf("pysix.Parse: json: %w", err)
	}

	if errMsg, ok := root["error"].(string); ok && errMsg != "" {
		return nil, fmt.Errorf("pysix.Parse: exporter: %s", errMsg)
	}

	return root, nil
}
