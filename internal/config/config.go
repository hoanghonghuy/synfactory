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
	Mode                  string
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
	WorkspaceRoot         string
	RuntimeConfigPath     string
	WorkerID              string
	WorkerCapacity        int
	WorkerPollInterval    time.Duration
	WorkerLeaseDuration   time.Duration
	WorkerHeartbeat       time.Duration
	WorkerDefaultTimeout  time.Duration
	WorkerRetryBase       time.Duration
	WorkerStaleAfter      time.Duration
	ShutdownTimeout       time.Duration
	LogLevel              string
}

func Load() (Config, error) {
	cfg := Config{
		Addr:                  envString("SYNFACTORY_ADDR", ":8080"),
		Mode:                  envString("SYNFACTORY_MODE", "all"),
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
		RepositoryRoot:        envString("SYNFACTORY_REPOSITORY_ROOT", "/var/lib/synfactory/repos"),
		WorkspaceRoot:         envString("SYNFACTORY_WORKSPACE_ROOT", "/var/lib/synfactory/workspaces"),
		RuntimeConfigPath:     envString("SYNFACTORY_RUNTIME_CONFIG", "/etc/synfactory/runtimes.json"),
		WorkerID:              os.Getenv("SYNFACTORY_WORKER_ID"),
		WorkerCapacity:        envIntPositive("SYNFACTORY_WORKER_CAPACITY", 1),
		WorkerPollInterval:    envDuration("SYNFACTORY_WORKER_POLL_INTERVAL", 3*time.Second),
		WorkerLeaseDuration:   envDuration("SYNFACTORY_WORKER_LEASE_DURATION", 2*time.Minute),
		WorkerHeartbeat:       envDuration("SYNFACTORY_WORKER_HEARTBEAT_INTERVAL", 30*time.Second),
		WorkerDefaultTimeout:  envDuration("SYNFACTORY_WORKER_DEFAULT_TIMEOUT", 30*time.Minute),
		WorkerRetryBase:       envDuration("SYNFACTORY_WORKER_RETRY_BASE", 30*time.Second),
		WorkerStaleAfter:      envDuration("SYNFACTORY_WORKER_STALE_AFTER", 2*time.Minute),
		ShutdownTimeout:       envDuration("SYNFACTORY_SHUTDOWN_TIMEOUT", 20*time.Second),
		LogLevel:              envString("SYNFACTORY_LOG_LEVEL", "info"),
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
