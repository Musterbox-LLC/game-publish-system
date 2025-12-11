package utils

import (
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
)

// EnsureUploadDir creates the uploads directory if it doesn't exist
func EnsureUploadDir() error {
	return os.MkdirAll("uploads", os.ModePerm)
}

// SaveFile saves the uploaded file to the given destination path
func SaveFile(fileHeader *multipart.FileHeader, destPath string) error {
	// ✅ Ensure the directory for the destination file exists
	dir := filepath.Dir(destPath)
	if err := os.MkdirAll(dir, os.ModePerm); err != nil {
		return err
	}

	file, err := fileHeader.Open()
	if err != nil {
		return err
	}
	defer file.Close()

	dst, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer dst.Close()

	_, err = io.Copy(dst, file)
	return err
}

// GetUploadPath returns the full path for a file inside the uploads directory
func GetUploadPath(filename string) string {
	return filepath.Join("uploads", filename)
}