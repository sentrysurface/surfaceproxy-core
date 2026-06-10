package config

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/fsnotify/fsnotify"
)

type Loader struct {
	mu      sync.RWMutex
	cfg     *Config
	path    string
	watcher *fsnotify.Watcher
}

// ResolvePath resolves the configuration file path. If the default "surface-proxy.json"
// is requested but does not exist in the current working directory, it falls back
// to ~/.surface-proxy/config.json.
func ResolvePath(path string) string {
	if path == "surface-proxy.json" {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			home, errHome := os.UserHomeDir()
			if errHome == nil {
				return filepath.Join(home, ".surface-proxy", "config.json")
			}
		}
	}
	return path
}

func NewLoader(path string) (*Loader, error) {
	resolvedPath := ResolvePath(path)
	l := &Loader{path: resolvedPath}
	cfg, err := l.load()
	if err != nil {
		return nil, err
	}
	l.cfg = cfg
	return l, nil
}

func (l *Loader) GetConfig() *Config {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.cfg
}

func (l *Loader) load() (*Config, error) {
	file, err := os.Open(l.path)
	if err != nil {
		if os.IsNotExist(err) {
			// Try to create the parent directories and write the default configuration
			if errDir := os.MkdirAll(filepath.Dir(l.path), 0755); errDir == nil {
				cfg := DefaultConfig()
				data, errMarshal := json.MarshalIndent(cfg, "", "  ")
				if errMarshal == nil {
					if errWrite := os.WriteFile(l.path, data, 0644); errWrite == nil {
						return cfg, nil
					}
				}
			}
			// Fallback: return default config in memory if writing fails
			return DefaultConfig(), nil
		}
		return nil, fmt.Errorf("failed to open config file: %w", err)
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config JSON: %w", err)
	}

	return &cfg, nil
}

func (l *Loader) Watch(onChange func(*Config)) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create fsnotify watcher: %w", err)
	}
	l.watcher = watcher

	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Has(fsnotify.Write) {
					cfg, err := l.load()
					if err != nil {
						// Drop invalid updates gracefully
						continue
					}
					l.mu.Lock()
					l.cfg = cfg
					l.mu.Unlock()
					if onChange != nil {
						onChange(cfg)
					}
				}
			case _, ok := <-watcher.Errors:
				if !ok {
					return
				}
			}
		}
	}()

	err = watcher.Add(l.path)
	if err != nil {
		watcher.Close()
		return fmt.Errorf("failed to watch path %s: %w", l.path, err)
	}

	return nil
}

func (l *Loader) Close() error {
	if l.watcher != nil {
		return l.watcher.Close()
	}
	return nil
}
