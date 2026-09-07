package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"

	"github.com/hoanghonghuy/synfactory/internal/authz"
	"github.com/hoanghonghuy/synfactory/internal/secrets"
)

const defaultRotationFileRoot = "/run/secrets-next"

type credentialRotationRequest struct {
	LogicalName string `json:"logical_name"`
	Source      string `json:"source,omitempty"`
}

type credentialRotationResponse struct {
	LogicalName string `json:"logical_name"`
	State       string `json:"state"`
}

func registerCredentialRotation(mux *http.ServeMux, authorizer authz.RequestAuthorizer, audit securityAuditWriter, provider *secrets.RotatingProvider) {
	if provider == nil {
		return
	}
	mux.HandleFunc("POST /api/security/credentials/rotation/stage", func(w http.ResponseWriter, r *http.Request) {
		principal, ok := authorizeCredentialRotation(w, r, authorizer)
		if !ok {
			return
		}
		request, err := decodeCredentialRotationRequest(r, true)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		candidate, err := rotationCandidateProvider(request.Source)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
			return
		}
		value, err := candidate.Resolve(r.Context(), request.LogicalName)
		if err != nil || len(strings.TrimSpace(string(value.CloneBytes()))) == 0 {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "rotation candidate is unavailable or invalid"})
			return
		}
		if err := appendSecurityAudit(r.Context(), audit, principal, "security.credentials.rotation.stage", "security_credential", request.LogicalName, "requested"); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "security audit unavailable"})
			return
		}
		if err := provider.Stage(r.Context(), request.LogicalName, candidate); err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "rotation candidate could not be staged"})
			return
		}
		writeJSON(w, http.StatusOK, credentialRotationResponse{LogicalName: request.LogicalName, State: "staged"})
	})

	mux.HandleFunc("POST /api/security/credentials/rotation/promote", func(w http.ResponseWriter, r *http.Request) {
		principal, ok := authorizeCredentialRotation(w, r, authorizer)
		if !ok {
			return
		}
		request, err := decodeCredentialRotationRequest(r, false)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if err := appendSecurityAudit(r.Context(), audit, principal, "security.credentials.rotation.promote", "security_credential", request.LogicalName, "requested"); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "security audit unavailable"})
			return
		}
		if err := provider.Promote(request.LogicalName); err != nil {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "no staged credential rotation"})
			return
		}
		writeJSON(w, http.StatusOK, credentialRotationResponse{LogicalName: request.LogicalName, State: "active"})
	})

	mux.HandleFunc("POST /api/security/credentials/rotation/discard", func(w http.ResponseWriter, r *http.Request) {
		principal, ok := authorizeCredentialRotation(w, r, authorizer)
		if !ok {
			return
		}
		request, err := decodeCredentialRotationRequest(r, false)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if err := appendSecurityAudit(r.Context(), audit, principal, "security.credentials.rotation.discard", "security_credential", request.LogicalName, "requested"); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "security audit unavailable"})
			return
		}
		if err := provider.DiscardStaged(request.LogicalName); err != nil {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "no staged credential rotation"})
			return
		}
		writeJSON(w, http.StatusOK, credentialRotationResponse{LogicalName: request.LogicalName, State: "discarded"})
	})
}

func authorizeCredentialRotation(w http.ResponseWriter, r *http.Request, authorizer authz.RequestAuthorizer) (authz.Principal, bool) {
	principal, err := authorizer.Authorize(r, authz.PermissionSecurityPolicy, "")
	if err != nil {
		status := http.StatusForbidden
		if errors.Is(err, authz.ErrUnauthenticated) {
			status = http.StatusUnauthorized
		}
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return authz.Principal{}, false
	}
	return principal, true
}

func decodeCredentialRotationRequest(r *http.Request, requireSource bool) (credentialRotationRequest, error) {
	var request credentialRotationRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return credentialRotationRequest{}, errors.New("invalid credential rotation request")
	}
	request.LogicalName = strings.TrimSpace(request.LogicalName)
	request.Source = strings.ToLower(strings.TrimSpace(request.Source))
	if !rotatableCredential(request.LogicalName) {
		return credentialRotationRequest{}, errors.New("logical_name is not a live-rotatable credential")
	}
	if requireSource && request.Source == "" {
		return credentialRotationRequest{}, errors.New("source is required")
	}
	if !requireSource && request.Source != "" {
		return credentialRotationRequest{}, errors.New("source is only valid for stage")
	}
	return request, nil
}

func rotatableCredential(logicalName string) bool {
	switch logicalName {
	case "operator/token", "github/webhook-secret":
		return true
	default:
		return false
	}
}

func rotationCandidateProvider(source string) (secrets.Provider, error) {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "env-next":
		return secrets.EnvProvider{Prefix: "SYNFACTORY_NEXT_"}, nil
	case "file-next":
		root := strings.TrimSpace(os.Getenv("SYNFACTORY_SECRET_ROTATION_FILE_ROOT"))
		if root == "" {
			root = defaultRotationFileRoot
		}
		return secrets.FileProvider{Root: root}, nil
	default:
		return nil, errors.New("unsupported rotation candidate source")
	}
}
