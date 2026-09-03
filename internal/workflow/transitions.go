package workflow

import "github.com/hoanghonghuy/synfactory/internal/domain"

func CanTransition(actor domain.Role, from, to State) bool {
	if from == to {
		return true
	}
	if from == StateCompleted || from == StateParked {
		return false
	}
	if to == StateBlocked {
		return actor == domain.RolePM || actor == domain.RoleTeamLead || actor == domain.RoleCIGuardian
	}
	if from == StateBlocked && (to == StateReady || to == StateImplementing || to == StateReviewing || to == StateVerifying) {
		return actor == domain.RolePM || actor == domain.RoleTeamLead
	}
	switch to {
	case StatePlanning:
		return actor == domain.RolePM || actor == domain.RoleTeamLead
	case StateReady:
		return actor == domain.RolePM || actor == domain.RoleTeamLead
	case StateImplementing:
		return actor == domain.RoleDev || actor == domain.RoleTeamLead
	case StateReviewing:
		return actor == domain.RoleReviewer || actor == domain.RoleTeamLead
	case StateVerifying:
		return actor == domain.RoleCIGuardian || actor == domain.RoleReviewer || actor == domain.RoleTeamLead
	case StateMergeGating:
		return actor == domain.RoleTeamLead
	case StateMergeReady:
		return actor == domain.RoleTeamLead || actor == domain.RoleReviewer
	case StateCompleted:
		return actor == domain.RoleTeamLead || actor == domain.RoleRelease
	case StateParked:
		return actor == domain.RolePM || actor == domain.RoleTeamLead
	default:
		return false
	}
}

func Advance(instance *Instance, actor domain.Role, to State) error {
	if instance == nil {
		return ErrInvalidTransition
	}
	if !CanTransition(actor, instance.State, to) {
		return ErrUnauthorizedActor
	}
	instance.State = to
	return nil
}
