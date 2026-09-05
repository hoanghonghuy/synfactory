package attention

import (
	"reflect"
	"testing"
	"time"
)

func TestEscalationRouterRoutesByRepositorySeverityAndAge(t *testing.T) {
	now := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)
	router := EscalationRouter{Rules: []EscalationRule{
		{MinSeverity: SeverityWarning, After: 15 * time.Minute, Providers: []string{"webhook"}},
		{RepositoryID: "repo-1", MinSeverity: SeverityCritical, After: 30 * time.Minute, Providers: []string{"pager", "webhook"}},
		{RepositoryID: "repo-2", MinSeverity: SeverityCritical, After: 5 * time.Minute, Providers: []string{"other"}},
	}}
	item := Item{
		RepositoryID: "repo-1",
		Severity:     SeverityCritical,
		State:        StateOpen,
		CreatedAt:    now.Add(-45 * time.Minute),
	}

	providers, err := router.Route(item, now)
	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}
	want := []string{"webhook", "pager"}
	if !reflect.DeepEqual(providers, want) {
		t.Fatalf("Route() providers = %#v, want %#v", providers, want)
	}
}

func TestEscalationRouterDoesNotEscalateYoungOrInactiveItems(t *testing.T) {
	now := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)
	router := EscalationRouter{Rules: []EscalationRule{{
		MinSeverity: SeverityWarning,
		After:       20 * time.Minute,
		Providers:   []string{"webhook"},
	}}}

	young := Item{Severity: SeverityCritical, State: StateOpen, CreatedAt: now.Add(-5 * time.Minute)}
	providers, err := router.Route(young, now)
	if err != nil {
		t.Fatalf("Route(young) error = %v", err)
	}
	if len(providers) != 0 {
		t.Fatalf("Route(young) providers = %#v, want none", providers)
	}

	resolved := Item{Severity: SeverityCritical, State: StateResolved, CreatedAt: now.Add(-time.Hour)}
	providers, err = router.Route(resolved, now)
	if err != nil {
		t.Fatalf("Route(resolved) error = %v", err)
	}
	if len(providers) != 0 {
		t.Fatalf("Route(resolved) providers = %#v, want none", providers)
	}
}

func TestEscalationRouterRejectsInvalidRules(t *testing.T) {
	now := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)
	item := Item{Severity: SeverityWarning, State: StateOpen, CreatedAt: now.Add(-time.Hour)}

	tests := []EscalationRouter{
		{Rules: []EscalationRule{{MinSeverity: Severity("urgent"), Providers: []string{"webhook"}}}},
		{Rules: []EscalationRule{{MinSeverity: SeverityWarning, After: -time.Second, Providers: []string{"webhook"}}}},
		{Rules: []EscalationRule{{MinSeverity: SeverityWarning, Providers: []string{" "}}}},
	}
	for index, router := range tests {
		if _, err := router.Route(item, now); err == nil {
			t.Fatalf("case %d: Route() error = nil, want validation error", index)
		}
	}
}
