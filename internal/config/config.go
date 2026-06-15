package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config represents the application settings
type Config struct {
	HostURL      string `yaml:"host_url"`
	CurrentModel string `yaml:"current_model"`
	Stream       bool   `yaml:"stream"`
	filePath     string
}

// Save writes the current configuration to disk
func (c *Config) Save() error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	dir := filepath.Dir(c.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(c.filePath, data, 0644)
}

// LoadConfig reads the configuration from either the local directory or the user's config directory
func LoadConfig() (*Config, error) {
	localPath := "config.yaml"
	if _, err := os.Stat(localPath); err == nil {
		return loadFromFile(localPath)
	}

	configDir, err := os.UserConfigDir()
	if err == nil {
		globalPath := filepath.Join(configDir, "askillama", "config.yaml")
		if _, err := os.Stat(globalPath); err == nil {
			return loadFromFile(globalPath)
		}
		// Default config file location when neither exists
		cfg := &Config{
			HostURL:      "http://localhost:11434",
			CurrentModel: "",
			Stream:       true,
			filePath:     globalPath,
		}
		if err := cfg.Save(); err != nil {
			// Fallback to local if global directory is unwritable
			cfg.filePath = localPath
			_ = cfg.Save()
		}
		return cfg, nil
	}

	cfg := &Config{
		HostURL:      "http://localhost:11434",
		CurrentModel: "",
		Stream:       true,
		filePath:     localPath,
	}
	_ = cfg.Save()
	return cfg, nil
}

func loadFromFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	cfg.Stream = true
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	cfg.filePath = path
	return &cfg, nil
}
