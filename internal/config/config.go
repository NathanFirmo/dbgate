package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

type Config struct {
	Port      int        `mapstructure:"port"`
	Databases []Database `mapstructure:"databases"`
}

type Database struct {
	Name     string `mapstructure:"name"`
	Type     string `mapstructure:"type"`
	DSN      string `mapstructure:"dsn"`
	URI      string `mapstructure:"uri"`
	Database string `mapstructure:"database"`
}

func Load() (Config, error) {
	v := viper.New()
	v.SetConfigName("dbgate")
	v.SetConfigType("yaml")
	v.AddConfigPath(".")
	v.SetDefault("port", 9999)
	v.SetEnvPrefix("DBGATE")
	v.AutomaticEnv()

	if configPath := strings.TrimSpace(v.GetString("config")); configPath != "" {
		v.SetConfigFile(configPath)
	}

	if err := v.ReadInConfig(); err != nil {
		var configFileNotFound viper.ConfigFileNotFoundError
		if !errors.As(err, &configFileNotFound) {
			return Config{}, fmt.Errorf("read config file: %w", err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return Config{}, fmt.Errorf("unmarshal config: %w", err)
	}

	if databasesJSON := strings.TrimSpace(v.GetString("databases")); databasesJSON != "" {
		if err := json.Unmarshal([]byte(databasesJSON), &cfg.Databases); err != nil {
			return Config{}, fmt.Errorf("parse DBGATE_DATABASES: %w", err)
		}
	}

	for i := range cfg.Databases {
		cfg.Databases[i].Name = strings.TrimSpace(cfg.Databases[i].Name)
		cfg.Databases[i].Type = strings.ToLower(strings.TrimSpace(cfg.Databases[i].Type))
		cfg.Databases[i].DSN = strings.TrimSpace(cfg.Databases[i].DSN)
		cfg.Databases[i].URI = strings.TrimSpace(cfg.Databases[i].URI)
		cfg.Databases[i].Database = strings.TrimSpace(cfg.Databases[i].Database)
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func (c Config) ListenAddress() string {
	return fmt.Sprintf(":%d", c.Port)
}

func (c Config) Validate() error {
	if c.Port <= 0 {
		return fmt.Errorf("port must be positive")
	}
	if len(c.Databases) == 0 {
		return fmt.Errorf("at least one database must be configured")
	}

	seen := make(map[string]struct{}, len(c.Databases))
	for _, db := range c.Databases {
		if err := db.Validate(); err != nil {
			return err
		}
		key := db.Identifier()
		if _, exists := seen[key]; exists {
			return fmt.Errorf("duplicate database %q", key)
		}
		seen[key] = struct{}{}
	}

	return nil
}

func (d Database) Identifier() string {
	return fmt.Sprintf("%s:%s", d.Type, d.Name)
}

func (d Database) Validate() error {
	if strings.TrimSpace(d.Name) == "" {
		return fmt.Errorf("database name is required")
	}

	switch strings.ToLower(strings.TrimSpace(d.Type)) {
	case "mysql":
		if strings.TrimSpace(d.DSN) == "" {
			return fmt.Errorf("mysql database %q requires dsn", d.Name)
		}
	case "mongo":
		if strings.TrimSpace(d.URI) == "" {
			return fmt.Errorf("mongo database %q requires uri", d.Name)
		}
		if strings.TrimSpace(d.Database) == "" {
			return fmt.Errorf("mongo database %q requires database", d.Name)
		}
	default:
		return fmt.Errorf("database %q has unsupported type %q", d.Name, d.Type)
	}

	return nil
}
