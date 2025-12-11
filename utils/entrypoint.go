// utils/entrypoint.go
package utils

import (
	"os"
	"path/filepath"
	"strings"
)

// Common WebGL entry point filenames (case-insensitive)
var entryCandidates = []string{
	"index.html",
	"index.htm",
	"main.html",
	"game.html",
	"play.html",
	"index.js", // fallback for JS-only bundles (rare)
}

func findEntryPoint(root string) (string, error) {
	var found string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		name := strings.ToLower(info.Name())
		for _, candidate := range entryCandidates {
			if name == candidate {
				// Return relative path from root
				rel, _ := filepath.Rel(root, path)
				found = filepath.ToSlash(rel) // Ensure forward slashes for URLs
				return filepath.SkipDir // stop early
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if found == "" {
		return "", os.ErrNotExist
	}
	return found, nil
}