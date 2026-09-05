package main

import (
	"net/http"

	"github.com/hoanghonghuy/synfactory/internal/authapi"
	"github.com/hoanghonghuy/synfactory/internal/authz"
	"github.com/hoanghonghuy/synfactory/internal/postgres"
)

func registerAuthAPI(mux *http.ServeMux, store *postgres.Store, authorizer authz.RequestAuthorizer) {
	handler := authapi.Handler{
		Store:      store,
		Authorizer: authorizer,
		Issuer:     authz.SessionIssuer{Store: store},
	}
	handler.Register(mux)
}
