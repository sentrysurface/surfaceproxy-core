package config

import (
	"io"
	"log"
	"os"
	"path/filepath"
)

// SetupLogging configures the default log package to output to both Stderr and
// a file named "surface-proxy.log" located in the same directory as the resolved config file.
func SetupLogging(resolvedConfigPath string) {
	configDir := filepath.Dir(resolvedConfigPath)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		log.Printf("[WARNING] Failed to create log directory %s: %v", configDir, err)
		return
	}

	logPath := filepath.Join(configDir, "surface-proxy.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Printf("[WARNING] Failed to open log file %s: %v", logPath, err)
		return
	}

	// Setup log output to write to both stderr and the log file
	mw := io.MultiWriter(os.Stderr, logFile)
	log.SetOutput(mw)
}
