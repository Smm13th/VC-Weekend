package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"vedhelper/internal/evalrun"
)

func main() {
	root, err := findBackendRoot()
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	r, err := evalrun.Run(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	_ = json.NewEncoder(os.Stdout).Encode(r)
	if r.Passed != r.Cases {
		os.Exit(1)
	}
}

func findBackendRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for dir := wd; ; dir = filepath.Dir(dir) {
		for _, candidate := range []string{dir, filepath.Join(dir, "backend")} {
			if _, err := os.Stat(filepath.Join(candidate, "eval", "cases.json")); err == nil {
				return candidate, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
	}
	return "", fmt.Errorf("cannot find eval/cases.json from %s; run from the project or backend directory, or pass the backend path", wd)
}
