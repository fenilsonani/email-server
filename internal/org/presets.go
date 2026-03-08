package org

// Preset defines a platform configuration preset.
type Preset struct {
	Label           string   `json:"label"`
	Description     string   `json:"description"`
	EnabledFeatures []string `json:"enabled_features"`
}

// Presets defines the available platform configuration presets.
var Presets = map[string]Preset{
	"email_only": {
		Label:       "Email Hosting",
		Description: "Full mailboxes with IMAP, SMTP, calendar",
		EnabledFeatures: []string{
			"users", "domains", "lists", "features",
			"queue", "security", "logs", "sieve",
		},
	},
	"api_only": {
		Label:       "Email API",
		Description: "Send transactional emails via REST API",
		EnabledFeatures: []string{
			"domains", "api_keys", "webhooks", "templates",
			"send_logs", "tracking", "suppression",
		},
	},
	"full": {
		Label:       "Full Platform",
		Description: "Everything: email hosting + API + lists",
		EnabledFeatures: []string{"*"},
	},
}

// HasFeature checks if a preset has a specific feature enabled.
func HasFeature(presetName, feature string) bool {
	preset, ok := Presets[presetName]
	if !ok {
		return false
	}
	for _, f := range preset.EnabledFeatures {
		if f == "*" || f == feature {
			return true
		}
	}
	return false
}

// GetPreset returns a preset by name or nil if not found.
func GetPreset(name string) *Preset {
	p, ok := Presets[name]
	if !ok {
		return nil
	}
	return &p
}
