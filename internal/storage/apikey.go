package storage

import (
	"os"
	"path/filepath"
	"strings"
)

// LoadAPIKey reads the API key from the given file path.
// Returns an empty string if the file does not exist.
func LoadAPIKey(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// SaveAPIKey writes the API key to the given file path, creating directories as needed.
func SaveAPIKey(path, key string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(key+"\n"), 0o600)
}
