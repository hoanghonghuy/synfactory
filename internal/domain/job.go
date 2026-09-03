package domain

import (
	"errors"
	"time"
)

type Role string

const (
	RolePM       Role = "pm"
	RoleTeamLead Role = "team_lead"
	RoleDev      Role = "developer"
	RoleReviewer Role = "reviewer"
	RoleQA       Role = "qa"
	RoleRelease  Role = "release"
)

type JobStatus string

const (
	JobQueued    JobStatus = "queued"
	JobLeased    JobStatus = "leased"
	JobRunning   JobStatus = "running"
	JobRetryWait JobStatus = "retry_wait"
	JobSucceeded JobStatus = "succeeded"
	JobFailed    JobStatus = "failed"
	JobCancelled JobStatus = "cancelled"
)

var (
	ErrInvalidTransition  = errors.New("invalid job state transition")
	ErrJobNotAvailable    = errors.New("job is not available yet")
	ErrLeaseOwnerMismatch = errors.New("lease owner mismatch")
)

type Job struct {
	ID          string
	Repository  string
	Kind        string
	Role        Role
	Subject     string
	Revision    string
	Priority    int
	Status      JobStatus
	Attempt     int
	MaxAttempts int
	AvailableAt time.Time
	LeaseOwner  string
	LeaseUntil  *time.Time
	LastError   string
}

func (j *Job) Lease(workerID string, now, until time.Time) error {
	if j.Status != JobQueued && j.Status != JobRetryWait {
		return ErrInvalidTransition
	}
	if now.Before(j.AvailableAt) {
		return ErrJobNotAvailable
	}
	if workerID == "" || !until.After(now) {
		return ErrInvalidTransition
	}

	j.Status = JobLeased
	j.LeaseOwner = workerID
	j.LeaseUntil = &until
	return nil
}

func (j *Job) Start(workerID string) error {
	if j.Status != JobLeased {
		return ErrInvalidTransition
	}
	if j.LeaseOwner != workerID {
		return ErrLeaseOwnerMismatch
	}
	if j.MaxAttempts <= 0 || j.Attempt >= j.MaxAttempts {
		return ErrInvalidTransition
	}

	j.Attempt++
	j.Status = JobRunning
	return nil
}

func (j *Job) Succeed(workerID string) error {
	if j.Status != JobRunning {
		return ErrInvalidTransition
	}
	if j.LeaseOwner != workerID {
		return ErrLeaseOwnerMismatch
	}

	j.Status = JobSucceeded
	j.clearLease()
	j.LastError = ""
	return nil
}

func (j *Job) Fail(workerID string, message string, retryAt time.Time) error {
	if j.Status != JobRunning {
		return ErrInvalidTransition
	}
	if j.LeaseOwner != workerID {
		return ErrLeaseOwnerMismatch
	}

	j.LastError = message
	j.clearLease()

	if j.Attempt < j.MaxAttempts {
		j.Status = JobRetryWait
		j.AvailableAt = retryAt
		return nil
	}

	j.Status = JobFailed
	return nil
}

func (j *Job) Requeue(now time.Time) error {
	if j.Status != JobRetryWait || now.Before(j.AvailableAt) {
		return ErrInvalidTransition
	}

	j.Status = JobQueued
	return nil
}

func (j Job) Terminal() bool {
	return j.Status == JobSucceeded || j.Status == JobFailed || j.Status == JobCancelled
}

func (j *Job) clearLease() {
	j.LeaseOwner = ""
	j.LeaseUntil = nil
}
