package config

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/fsnotify/fsnotify"
)

type Loader struct {
	mu      sync.RWMutex
	cfg     *Config
	path    string
	watcher *fsnotify.Watcher
}

func NewLoader(path string) (*Loader, error) {
	l := &Loader{path: path}
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
