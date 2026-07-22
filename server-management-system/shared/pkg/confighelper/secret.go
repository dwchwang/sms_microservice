package confighelper

import (
	"os"
	"strings"
)

// GetStringSecret returns the configuration string for the given envName.
// It first checks if <envName>_FILE is set in the environment and points to a readable file.
// If so, it returns the trimmed content of that file (useful for Docker Secrets).
// Otherwise, it returns the fallback value.
func GetStringSecret(envName string, fallback string) string {
	fileEnv := envName + "_FILE"
	if filePath := os.Getenv(fileEnv); filePath != "" {
		if b, err := os.ReadFile(filePath); err == nil {
			return strings.TrimSpace(string(b))
		}
	}
	return fallback
}
