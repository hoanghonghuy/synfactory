package workflow

import (
	"crypto/sha256"
	"encoding/hex"
)

func NewInstance(repositoryID string, kind Kind, subject, revision string, priority int) Instance {
	seed := repositoryID + "\x00" + string(kind) + "\x00" + subject
	sum := sha256.Sum256([]byte(seed))
	digest := hex.EncodeToString(sum[:])
	if priority == 0 {
		priority = 100
	}
	return Instance{
		ID:                "wf_" + digest[:24],
		DedupeKey:         "workflow:" + digest,
		RepositoryID:      repositoryID,
		Kind:              kind,
		Subject:           subject,
		Revision:          revision,
		State:             StateDiscovered,
		Priority:          priority,
		CIRepairLimit:     2,
		ReviewRepairLimit: 2,
	}
}
