package util

import (
	"io"
	"os"
)

func DeleteFile(filePath string) error {
	if _, err := os.Stat(filePath); err != nil {
		return err
	}

	if err := os.Remove(filePath); err != nil {
		return err
	}

	return nil
}

func DownloadFileToTempDirectory(data io.Reader) (*os.File, error) {
	tempFile, err := os.CreateTemp("", "discordfile-")
	if err != nil {
		return nil, err
	}

	if _, err = io.Copy(tempFile, data); err != nil {
		return nil, err
	}

	if _, err = tempFile.Seek(0, 0); err != nil {
		return nil, err
	}

	return tempFile, nil
}
