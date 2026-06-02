//this file's purpose is to parse the lb.yaml's plain text into executable Go code

package config

import (
	"fmt"

	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Listen     string     `yaml:"listen"`
	Backends   []Backend  `yaml:"backends"`
	Health     Health     `yaml:"health"`
	Stickiness Stickiness `yaml:"stickiness"`
	Admin      Admin      `yaml:"admin"`
}

type Backend struct {
	Addr string `yaml:"addr"`
}

type Health struct {
	Path          string `yaml:"path"`
	IntervalS     int    `yaml:"interval_s"`
	FailThreshold int    `yaml:"fail_threshold"`
	PassThreshold int    `yaml:"pass_threshold"`
}

type Stickiness struct {
	RoomIDRegex string `yaml:"room_id_regex"`
}

type Admin struct {
	Listen       string `yaml:"listen"`
	AuthTokenEnv string `yaml:"auth_token_env"`
}

func (c *Config) validateYamlValues() error {
	if c.Listen == "" {
		return fmt.Errorf("error, listen address is necessary")
	}
	if len(c.Backends) == 0 {
		return fmt.Errorf("error, at least one backend is required")
	}
	for _, b := range c.Backends {
		if b.Addr == "" {
			return fmt.Errorf("error, backend address cannot be empty")
		}
	}
	if c.Health.Path == "" {
		return fmt.Errorf("error, health.path is missing")
	}
	if c.Health.IntervalS <= 0 {
		return fmt.Errorf("error, health.interval_s must be over 0")
	}
	if c.Health.FailThreshold <= 0 {
		return fmt.Errorf("error, health.fail_threshold must be over 0")
	}
	if c.Health.PassThreshold <= 0 {
		return fmt.Errorf("error, health.pass_threshold must be over 0")
	}
	if c.Stickiness.RoomIDRegex == "" {
		return fmt.Errorf("error, stickiness.room_id_regex is missing")
	}
	if c.Admin.Listen == "" {
		return fmt.Errorf("error, admin.listen is missing")
	}
	return nil
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("error, failed to read config - %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("error, parsing config - %w", err)
	}
	if err := cfg.validateYamlValues(); err != nil {
		return nil, fmt.Errorf("error, invalid config - %w", err)
	}
	return &cfg, nil
}
