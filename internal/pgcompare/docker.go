package pgcompare

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
)

const (
	defaultMigrationVersionEnv = "MIGRATION_VERSION"
	defaultDockerComposeEnv    = "DOCKER_COMPOSE"
)

type dockerComparator struct {
	log        *slog.Logger
	cfg        *Config
	composeCmd []string
}

func NewDockerComparator(log *slog.Logger, cfg *Config) (*dockerComparator, error) {
	dockerCompose, err := detectDockerCompose()
	if err != nil {
		return nil, fmt.Errorf("detect docker compose: %w", err)
	}
	return &dockerComparator{log: log, composeCmd: dockerCompose, cfg: cfg}, nil
}

func (d *dockerComparator) Cleanup(ctx context.Context) error {
	args := append(append([]string{}, d.composeCmd...), "down", "-v")
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)

	cmd.Dir = d.cfg.ProjectDir
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout

	d.log.Info("cleanup docker environment", "cmd", strings.Join(args, " "), "dir", cmd.Dir)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("cleanup docker environment: %w", err)
	}

	return nil
}

func (d *dockerComparator) PrepareVersion(ctx context.Context, version string) error {
	d.log.Info("prepare docker version", "version", version)

	if err := d.Cleanup(ctx); err != nil {
		return fmt.Errorf("prepare docker version: %w", err)
	}

	if err := d.runSetup(ctx, version); err != nil {
		return fmt.Errorf("run docker setup: %w", err)
	}

	return nil
}

func (d *dockerComparator) runSetup(ctx context.Context, version string) error {
	command := d.buildSetupCommand()

	cmd := shellCommand(ctx, command)
	cmd.Dir = d.cfg.ProjectDir

	env := os.Environ()
	env = envWithOverride(env, defaultMigrationVersionEnv, version)
	cmd.Env = env

	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("run setup for migration version %s: %w", version, err)
	}

	return nil
}

func (d *dockerComparator) buildSetupCommand() string {
	command := d.cfg.Setup.Command
	compose := strings.Join(d.composeCmd, " ")

	command = strings.ReplaceAll(command, fmt.Sprintf("$%s", defaultDockerComposeEnv), compose)
	command = strings.ReplaceAll(command, fmt.Sprintf("${%s}", defaultDockerComposeEnv), compose)

	return command
}
