// utils/unzip.go
package utils

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Unzip extracts a zip file to the given destination directory.
// Returns an error if any file tries to escape the destination (path traversal protection).
func Unzip(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		// Build the full path for extraction
		path := filepath.Join(dest, f.Name)

		// ✅ Security: prevent zip slip (path traversal)
		if !strings.HasPrefix(path, filepath.Clean(dest)+string(os.PathSeparator)) {
			return fmt.Errorf("illegal file path: %s", f.Name)
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(path, os.ModePerm); err != nil {
				return err
			}
			continue
		}

		// Ensure parent directories exist
		if err := os.MkdirAll(filepath.Dir(path), os.ModePerm); err != nil {
			return err
		}

		// Open the destination file
		outFile, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}

		// Open the file inside the zip
		rc, err := f.Open()
		if err != nil {
			outFile.Close()
			return err
		}

		// Copy contents
		_, err = io.Copy(outFile, rc)

		// Close both files
		outFile.Close()
		rc.Close()

		if err != nil {
			return err
		}
	}

	return nil
}
