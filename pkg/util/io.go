package util

import (
	"fmt"
	"io"
	"os"
)

func DeleteFile(filePath string) error {
	if _, err := os.Stat(filePath); err != nil {
		return fmt.Errorf("describing file path: %w", err)
	}

	if err := os.Remove(filePath); err != nil {
		return fmt.Errorf("removing file path: %w", err)
	}

	return nil
}

func DownloadFileToTempDirectory(data io.Reader) (*os.File, error) {
	tempFile, err := os.CreateTemp("", "discordfile-*.m4a")
	if err != nil {
		return nil, fmt.Errorf("creating temp file: %w", err)
	}

	if _, err = io.Copy(tempFile, data); err != nil {
		return nil, fmt.Errorf("copying file content: %w", err)
	}

	if _, err = tempFile.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("seeking file to seek.start: %w", err)
	}

	return tempFile, nil
}
