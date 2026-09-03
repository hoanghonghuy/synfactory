package config

import (
	"errors"
	"os"
	"strconv"
	"time"
)

var ErrDatabaseURLRequired = errors.New("SYNFACTORY_DATABASE_URL is required")

type Config struct {
	Addr                  string
	DatabaseURL           string
	DBMaxOpenConns        int
	DBMaxIdleConns        int
	DBConnMaxIdle         time.Duration
	DBConnMaxLifetime     time.Duration
	GitHubAPIURL          string
	GitHubToken           string
	GitHubWebhookSecret   string
	ReconcileInterval     time.Duration
	EventPollInterval     time.Duration
	EventLeaseDuration    time.Duration
	EventMaxAttempts      int
	LeaseRecoveryInterval time.Duration
	WorkflowInterval      time.Duration
	WorkflowPMWIP         int
	WorkflowTeamLeadWIP   int
	WorkflowDevWIP        int
	WorkflowReviewerWIP   int
	WorkflowCIGuardianWIP int
	TaskReservationTTL    time.Duration
	RepositoryRoot        string
}

func Load() (Config, error) {
	cfg := Config{
		Addr:                  envString("SYNFACTORY_ADDR", ":8080"),
		DatabaseURL:           os.Getenv("SYNFACTORY_DATABASE_URL"),
		DBMaxOpenConns:        envInt("SYNFACTORY_DB_MAX_OPEN_CONNS", 20),
		DBMaxIdleConns:        envInt("SYNFACTORY_DB_MAX_IDLE_CONNS", 5),
		DBConnMaxIdle:         envDuration("SYNFACTORY_DB_CONN_MAX_IDLE", 5*time.Minute),
		DBConnMaxLifetime:     envDuration("SYNFACTORY_DB_CONN_MAX_LIFETIME", 30*time.Minute),
		GitHubAPIURL:          envString("SYNFACTORY_GITHUB_API_URL", "https://api.github.com"),
		GitHubToken:           os.Getenv("SYNFACTORY_GITHUB_TOKEN"),
		GitHubWebhookSecret:   os.Getenv("SYNFACTORY_GITHUB_WEBHOOK_SECRET"),
		ReconcileInterval:     envDuration("SYNFACTORY_RECONCILE_INTERVAL", time.Hour),
		EventPollInterval:     envDuration("SYNFACTORY_EVENT_POLL_INTERVAL", 5*time.Second),
		EventLeaseDuration:    envDuration("SYNFACTORY_EVENT_LEASE_DURATION", 30*time.Second),
		EventMaxAttempts:      envIntPositive("SYNFACTORY_EVENT_MAX_ATTEMPTS", 5),
		LeaseRecoveryInterval: envDuration("SYNFACTORY_LEASE_RECOVERY_INTERVAL", 30*time.Second),
		WorkflowInterval:      envDuration("SYNFACTORY_WORKFLOW_INTERVAL", time.Minute),
		WorkflowPMWIP:         envIntPositive("SYNFACTORY_WIP_PM", 1),
		WorkflowTeamLeadWIP:   envIntPositive("SYNFACTORY_WIP_TEAM_LEAD", 2),
		WorkflowDevWIP:        envIntPositive("SYNFACTORY_WIP_DEVELOPER", 2),
		WorkflowReviewerWIP:   envIntPositive("SYNFACTORY_WIP_REVIEWER", 2),
		WorkflowCIGuardianWIP: envIntPositive("SYNFACTORY_WIP_CI_GUARDIAN", 1),
		TaskReservationTTL:    envDuration("SYNFACTORY_TASK_RESERVATION_TTL", 10*time.Minute),
		RepositoryRoot:        os.Getenv("SYNFACTORY_REPOSITORY_ROOT"),
	}
	if cfg.DatabaseURL == "" {
		return Config{}, ErrDatabaseURLRequired
	}
	return cfg, nil
}

func envString(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return fallback
	}
	return parsed
}

func envIntPositive(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func envDuration(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
