package pgcompare

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"
	"gopkg.in/yaml.v3"
)

type DescriptionEntry struct {
	Query    string `yaml:"query"`
	What     string `yaml:"what"`
	Changes  string `yaml:"changes"`
	Expected string `yaml:"expected"`
}

type Config struct {
	ProjectDir string

	Migration struct {
		EnvVar        string `yaml:"env_var"`
		BeforeVersion string `yaml:"before_version"`
		AfterVersion  string `yaml:"after_version"`
	} `yaml:"migration"`

	Setup struct {
		Command string `yaml:"command"`
	} `yaml:"setup"`

	Benchmark struct {
		BeforeQueries    string `yaml:"before_queries"`
		AfterQueries     string `yaml:"after_queries"`
		WarmupIterations int    `yaml:"warmup_iterations" default:"0"`
		Iterations       int    `yaml:"iterations"`
		Concurrency      int    `yaml:"concurrency"`
		Repeats          int    `yaml:"repeats"`
	} `yaml:"benchmark"`

	Report struct {
		Description []DescriptionEntry `yaml:"description"`
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
	cfg.Migration.EnvVar = normalizeMigrationEnvVar(cfg.Migration.EnvVar)
	if cfg.Benchmark.Repeats == 0 {
		cfg.Benchmark.Repeats = 1
	}

	if err := godotenv.Load(filepath.Join(projectDir, ".env")); err != nil {
		return nil, fmt.Errorf("load .env: %w", err)
	}

	cfg.DSN = buildDSN()

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func normalizeMigrationEnvVar(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return defaultMigrationVersionEnv
	}
	return value
}

func (c *Config) validate() error {
	if c.Migration.EnvVar == "" {
		return fmt.Errorf("migration.env_var must not be empty")
	}
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
	if c.Benchmark.WarmupIterations < 0 {
		return fmt.Errorf("benchmark.warmup_iterations must be non-negative")
	}
	if c.Benchmark.Iterations <= 0 {
		return fmt.Errorf("benchmark.iterations must be positive")
	}
	if c.Benchmark.Concurrency <= 0 {
		return fmt.Errorf("benchmark.concurrency must be positive")
	}
	if c.Benchmark.Repeats <= 0 {
		return fmt.Errorf("benchmark.repeats must be positive")
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
