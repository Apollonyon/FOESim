package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
)

// PrivacyConfig holds operator-supplied deployment metadata served by GET /api/privacy.
// Keeping these values outside embedded frontend assets ensures self-hosters do not
// inherit another operator's legal identity or hosting claims.
type PrivacyConfig struct {
	Operator             string `json:"operator,omitempty"`
	PostalAddress        string `json:"postal_address,omitempty"`
	ContactEmail         string `json:"contact_email,omitempty"`
	HostingProvider      string `json:"hosting_provider,omitempty"`
	HostingLogRetention  string `json:"hosting_log_retention,omitempty"`
	DataTransferNote     string `json:"data_transfer_note,omitempty"`
	SupervisoryAuthority string `json:"supervisory_authority,omitempty"`
	LastUpdated          string `json:"last_updated,omitempty"`
}

type privacyPayload struct {
	Configured bool `json:"configured"`
	PrivacyConfig
}

func isPlaceholderValue(s string) bool {
	trimmed := strings.TrimSpace(s)
	return trimmed == "" || (strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]"))
}

// IsConfigured reports whether the operator has supplied a real controller name and contact email.
func (c PrivacyConfig) IsConfigured() bool {
	return !isPlaceholderValue(c.Operator) && !isPlaceholderValue(c.ContactEmail)
}

func cleanField(s string) string {
	if isPlaceholderValue(s) {
		return ""
	}
	return strings.TrimSpace(s)
}

func (c PrivacyConfig) sanitized() PrivacyConfig {
	return PrivacyConfig{
		Operator:             cleanField(c.Operator),
		PostalAddress:        cleanField(c.PostalAddress),
		ContactEmail:         cleanField(c.ContactEmail),
		HostingProvider:      cleanField(c.HostingProvider),
		HostingLogRetention:  cleanField(c.HostingLogRetention),
		DataTransferNote:     cleanField(c.DataTransferNote),
		SupervisoryAuthority: cleanField(c.SupervisoryAuthority),
		LastUpdated:          cleanField(c.LastUpdated),
	}
}

// LoadPrivacyConfig reads an optional JSON file (ignored when missing unless explicit is true)
// and applies FOES_PRIVACY_* environment variable overrides.
func LoadPrivacyConfig(path string, explicit bool) (PrivacyConfig, error) {
	var cfg PrivacyConfig
	cleanPath := strings.TrimSpace(path)
	if cleanPath != "" {
		raw, err := os.ReadFile(cleanPath)
		if err != nil {
			if explicit || !errors.Is(err, os.ErrNotExist) {
				return PrivacyConfig{}, fmt.Errorf("read privacy config %q: %w", cleanPath, err)
			}
		} else if err := json.Unmarshal(raw, &cfg); err != nil {
			return PrivacyConfig{}, fmt.Errorf("parse privacy config %q: %w", cleanPath, err)
		}
	}

	applyEnv := func(envKey string, dst *string) {
		if v, ok := os.LookupEnv(envKey); ok {
			*dst = v
		}
	}
	applyEnv("FOES_PRIVACY_OPERATOR", &cfg.Operator)
	applyEnv("FOES_PRIVACY_POSTAL_ADDRESS", &cfg.PostalAddress)
	applyEnv("FOES_PRIVACY_CONTACT_EMAIL", &cfg.ContactEmail)
	applyEnv("FOES_PRIVACY_HOSTING_PROVIDER", &cfg.HostingProvider)
	applyEnv("FOES_PRIVACY_HOSTING_LOG_RETENTION", &cfg.HostingLogRetention)
	applyEnv("FOES_PRIVACY_DATA_TRANSFER_NOTE", &cfg.DataTransferNote)
	applyEnv("FOES_PRIVACY_SUPERVISORY_AUTHORITY", &cfg.SupervisoryAuthority)
	applyEnv("FOES_PRIVACY_LAST_UPDATED", &cfg.LastUpdated)

	return cfg.sanitized(), nil
}

// SetPrivacyConfig configures the operator privacy notice returned by GET /api/privacy.
// Must be called before serving requests.
func (s *Server) SetPrivacyConfig(cfg PrivacyConfig) {
	s.privacy = cfg.sanitized()
}

func (s *Server) handlePrivacy(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	payload := privacyPayload{Configured: s.privacy.IsConfigured()}
	if payload.Configured {
		payload.PrivacyConfig = s.privacy
	}
	_ = json.NewEncoder(w).Encode(payload)
}
