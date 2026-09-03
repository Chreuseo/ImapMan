package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Server   struct{ Address string }
	Database struct {
		Driver string
		DSN    string
	}
	APISecret string
	SMTP      struct {
		Host        string
		Port        int
		Username    string
		PasswordEnv string
		From        string
	}
	Delivery struct {
		BatchSize    int
		UseBCC       bool
		ToHeader     string
		DelayBetween time.Duration
	}
	Processing struct {
		PollInterval    time.Duration
		ReprocessFailed bool
		MaxAttempts     int
	}
}

func Load() (Config, error) {
	var cfg Config
	cfg.Server.Address = envOr("IMAPMAN_SERVER_ADDRESS", ":8080")
	cfg.Database.Driver = envOr("IMAPMAN_DATABASE_DRIVER", "mysql")
	cfg.Database.DSN = os.Getenv("IMAPMAN_DATABASE_DSN")
	cfg.APISecret = os.Getenv("IMAPMAN_API_SECRET")
	cfg.SMTP.Host = os.Getenv("IMAPMAN_SMTP_HOST")
	cfg.SMTP.Username = os.Getenv("IMAPMAN_SMTP_USERNAME")
	cfg.SMTP.PasswordEnv = os.Getenv("IMAPMAN_SMTP_PASSWORD_ENV")
	cfg.SMTP.From = os.Getenv("IMAPMAN_SMTP_FROM")
	var err error
	if cfg.Database.DSN == "" || cfg.APISecret == "" {
		return cfg, fmt.Errorf("IMAPMAN_DATABASE_DSN and IMAPMAN_API_SECRET must be set")
	}
	if cfg.Database.Driver != "mysql" {
		return cfg, fmt.Errorf("IMAPMAN_DATABASE_DRIVER must be mysql for MariaDB")
	}
	if cfg.SMTP.Port, err = envInt("IMAPMAN_SMTP_PORT", 0); err != nil {
		return cfg, err
	}
	if cfg.Delivery.BatchSize, err = envInt("IMAPMAN_DELIVERY_BATCH_SIZE", 50); err != nil {
		return cfg, err
	}
	if cfg.Delivery.UseBCC, err = envBool("IMAPMAN_DELIVERY_USE_BCC", true); err != nil {
		return cfg, err
	}
	cfg.Delivery.ToHeader = envOr("IMAPMAN_DELIVERY_TO_HEADER", "undisclosed")
	if cfg.Delivery.DelayBetween, err = envDuration("IMAPMAN_DELIVERY_DELAY_BETWEEN", 0); err != nil {
		return cfg, err
	}
	if cfg.Processing.PollInterval, err = envDuration("IMAPMAN_PROCESSING_POLL_INTERVAL", time.Minute); err != nil {
		return cfg, err
	}
	if cfg.Processing.ReprocessFailed, err = envBool("IMAPMAN_PROCESSING_REPROCESS_FAILED", true); err != nil {
		return cfg, err
	}
	if cfg.Processing.MaxAttempts, err = envInt("IMAPMAN_PROCESSING_MAX_ATTEMPTS", 3); err != nil {
		return cfg, err
	}
	if cfg.Delivery.BatchSize < 1 || cfg.Processing.MaxAttempts < 1 || (cfg.Delivery.ToHeader != "from" && cfg.Delivery.ToHeader != "undisclosed") {
		return cfg, fmt.Errorf("invalid delivery or processing configuration")
	}
	return cfg, nil
}

func Secret(env string) (string, error) {
	value := os.Getenv(env)
	if value == "" {
		return "", fmt.Errorf("required environment variable %q is not set", env)
	}
	return value, nil
}
func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
func envInt(name string, fallback int) (int, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", name, err)
	}
	return parsed, nil
}
func envBool(name string, fallback bool) (bool, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean: %w", name, err)
	}
	return parsed, nil
}
func envDuration(name string, fallback time.Duration) (time.Duration, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be a duration: %w", name, err)
	}
	return parsed, nil
}
