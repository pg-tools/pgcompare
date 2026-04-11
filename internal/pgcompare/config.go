package pgcompare

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
	"gopkg.in/yaml.v3"
)

type Config struct {
	ProjectDir string

	Migration struct {
		BeforeVersion string `yaml:"before_version"`
		AfterVersion  string `yaml:"after_version"`
	} `yaml:"migration"`

	Setup struct {
		Command string `yaml:"command"`
	} `yaml:"setup"`

	Benchmark struct {
		BeforeQueries string `yaml:"before_queries"`
		AfterQueries  string `yaml:"after_queries"`
		Iterations    int    `yaml:"iterations"`
		Concurrency   int    `yaml:"concurrency"`
	} `yaml:"benchmark"`

	Report struct {
		Description string `yaml:"description"`
	} `yaml:"report"`

	DSN string
}

func LoadConfig(configPath string) (*Config, error) {
	absPath, err := filepath.Abs(configPath)
	if err != nil {
		return nil, fmt.Errorf("resolve config path: %w", err)
	}

	projectDir := filepath.Dir(absPath)

	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	cfg.ProjectDir = projectDir

	if err := godotenv.Load(filepath.Join(projectDir, ".env")); err != nil {
		return nil, fmt.Errorf("load .env: %w", err)
	}

	cfg.DSN = buildDSN()

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func (c *Config) validate() error {
	if c.Migration.BeforeVersion == "" {
		return fmt.Errorf("migration.before_version is required")
	}
	if c.Migration.AfterVersion == "" {
		return fmt.Errorf("migration.after_version is required")
	}
	if c.Setup.Command == "" {
		return fmt.Errorf("setup.command is required")
	}
	if c.Benchmark.BeforeQueries == "" {
		return fmt.Errorf("benchmark.before_queries is required")
	}
	if c.Benchmark.AfterQueries == "" {
		return fmt.Errorf("benchmark.after_queries is required")
	}
	if c.Benchmark.Iterations <= 0 {
		return fmt.Errorf("benchmark.iterations must be positive")
	}
	if c.Benchmark.Concurrency <= 0 {
		return fmt.Errorf("benchmark.concurrency must be positive")
	}
	return nil
}

func buildDSN() string {
	port := os.Getenv("POSTGRES_PORT")
	if port == "" {
		port = "5432"
	}
	return fmt.Sprintf(
		"postgres://%s:%s@localhost:%s/%s",
		os.Getenv("POSTGRES_USER"),
		os.Getenv("POSTGRES_PASSWORD"),
		port,
		os.Getenv("POSTGRES_DB"),
	)
}
