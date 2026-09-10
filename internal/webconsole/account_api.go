package webconsole

import (
	"context"
	"log/slog"
	"net/http"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"time"

	"venuewire/internal/accountstate"
	"venuewire/internal/domain"
)

type AccountService interface {
	Snapshot(domain.Venue) (accountstate.Snapshot, bool)
	Refresh(context.Context, domain.Venue) error
	Health() []accountstate.VenueHealth
}

type BuildMetadata struct {
	Timestamp string
	Commit    string
}

type publicVenueStatus struct {
	Venue           domain.Venue `json:"venue"`
	REST            string       `json:"rest"`
	PublicWS        string       `json:"publicWs"`
	PrivateWS       string       `json:"privateWs"`
	AccountSync     string       `json:"accountSync"`
	AccountAgeMS    *int64       `json:"accountAgeMs,omitempty"`
	MarketAgeMS     *int64       `json:"marketAgeMs,omitempty"`
	Reconnects      uint64       `json:"reconnects"`
	LastReconcileAt string       `json:"lastReconcileAt,omitempty"`
	RequestErrors   uint64       `json:"requestErrors"`
	LastRequestRTT  *int64       `json:"lastRequestRttMs,omitempty"`
	Discrepancies   uint64       `json:"reconciliationDiscrepancies"`
}

func (s *Server) handleAccount(w http.ResponseWriter, r *http.Request) {
	venue, ok := s.enabledVenue(r.PathValue("venue"))
	if !ok {
		writeError(w, http.StatusNotFound, "venue_not_found", "The venue is not available.", requestID(r))
		return
	}
	if s.accounts == nil {
		writeError(w, http.StatusServiceUnavailable, "account_unavailable", "Account data is temporarily unavailable.", requestID(r))
		return
	}
	snapshot, exists := s.accounts.Snapshot(venue)
	if !exists {
		writeError(w, http.StatusServiceUnavailable, "account_syncing", "The account snapshot is not available yet.", requestID(r))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"account": snapshot})
}

func (s *Server) handleAccountRefresh(w http.ResponseWriter, r *http.Request) {
	venue, ok := s.enabledVenue(r.PathValue("venue"))
	if !ok {
		writeError(w, http.StatusNotFound, "venue_not_found", "The venue is not available.", requestID(r))
		return
	}
	if s.accounts == nil {
		writeError(w, http.StatusServiceUnavailable, "account_unavailable", "Account data is temporarily unavailable.", requestID(r))
		return
	}
	if err := s.accounts.Refresh(r.Context(), venue); err != nil {
		s.logger.Error("account refresh failed", slog.String("requestId", requestID(r)), slog.String("venue", string(venue)), slog.String("error", err.Error()))
		writeError(w, http.StatusServiceUnavailable, "account_refresh_failed", "The account could not be refreshed. The last known snapshot remains available.", requestID(r))
		return
	}
	snapshot, exists := s.accounts.Snapshot(venue)
	if !exists {
		writeError(w, http.StatusServiceUnavailable, "account_syncing", "The account snapshot is not available yet.", requestID(r))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"account": snapshot})
}

func (s *Server) handleSystemStatus(w http.ResponseWriter, _ *http.Request) {
	now := s.now()
	statuses := s.publicStatuses(now)
	uptime := now.Sub(s.startedAt)
	if uptime < 0 {
		uptime = 0
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"venues": statuses,
		"build": map[string]string{
			"environment":    "Testnet",
			"backend":        "Go " + runtime.Version(),
			"frontend":       "Vue 3",
			"buildTimestamp": displayMetadata(s.build.Timestamp),
			"gitCommit":      displayMetadata(s.build.Commit),
			"uptime":         uptime.Truncate(time.Second).String(),
		},
	})
}

func (s *Server) publicStatuses(now time.Time) []publicVenueStatus {
	health := s.configuredHealth()
	statuses := make([]publicVenueStatus, 0, len(health))
	for _, item := range health {
		status := publicVenueStatus{
			Venue: item.Venue, REST: item.REST, PublicWS: item.PublicWS, PrivateWS: item.PrivateWS,
			AccountSync: item.AccountSync, Reconnects: item.Reconnects, RequestErrors: item.RequestErrors,
			Discrepancies: item.Discrepancies,
		}
		if !item.MarketEventAt.IsZero() {
			age := nonNegativeMilliseconds(now.Sub(item.MarketEventAt))
			status.MarketAgeMS = &age
		}
		if !item.LastReconcileAt.IsZero() {
			status.LastReconcileAt = item.LastReconcileAt.UTC().Format(time.RFC3339Nano)
		}
		if item.LastRequestRTT > 0 {
			rtt := nonNegativeMilliseconds(item.LastRequestRTT)
			status.LastRequestRTT = &rtt
		}
		if s.accounts != nil {
			if snapshot, ok := s.accounts.Snapshot(item.Venue); ok && !snapshot.SnapshotAsOf.IsZero() {
				age := nonNegativeMilliseconds(now.Sub(snapshot.SnapshotAsOf))
				status.AccountAgeMS = &age
			}
		}
		statuses = append(statuses, status)
	}
	return statuses
}

func (s *Server) configuredHealth() []accountstate.VenueHealth {
	byVenue := make(map[domain.Venue]accountstate.VenueHealth)
	if s.accounts != nil {
		for _, item := range s.accounts.Health() {
			byVenue[item.Venue] = item
		}
	}
	result := make([]accountstate.VenueHealth, 0, 2)
	for _, venue := range []domain.Venue{domain.VenueBybit, domain.VenueDeribit} {
		if _, enabled := s.enabledVenue(string(venue)); !enabled {
			continue
		}
		item, exists := byVenue[venue]
		if !exists {
			item = accountstate.VenueHealth{Venue: venue, REST: "UNAVAILABLE", PublicWS: "UNAVAILABLE", PrivateWS: "UNAVAILABLE", AccountSync: "UNAVAILABLE"}
		}
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Venue < result[j].Venue })
	return result
}

func (s *Server) enabledVenue(raw string) (domain.Venue, bool) {
	venue := domain.Venue(strings.ToLower(strings.TrimSpace(raw)))
	switch venue {
	case domain.VenueBybit:
		return venue, s.config.BybitEnabled
	case domain.VenueDeribit:
		return venue, s.config.DeribitEnabled
	default:
		return "", false
	}
}

func runtimeBuildMetadata() BuildMetadata {
	metadata := BuildMetadata{}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return metadata
	}
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			metadata.Commit = setting.Value
		case "vcs.time":
			metadata.Timestamp = setting.Value
		}
	}
	return metadata
}

func displayMetadata(value string) string {
	if strings.TrimSpace(value) == "" {
		return "development"
	}
	return value
}

func nonNegativeMilliseconds(value time.Duration) int64 {
	if value < 0 {
		return 0
	}
	return value.Milliseconds()
}
