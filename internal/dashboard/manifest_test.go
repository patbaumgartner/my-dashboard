package dashboard

import (
	"net/url"
	"strings"
	"testing"
)

const validPresentationPrefix = `"lifecycleFilters":[{"id":"all","label":"All services","shortLabel":"All"}],"categories":[{"id":"infrastructure","title":"Infrastructure","description":"Core services"},{"id":"tools","title":"Tools","description":"Utilities"},{"id":"automation","title":"Automation","description":"Workers"}],`

func TestExampleManifestIsValid(t *testing.T) {
	manifest, err := LoadManifestFile("../../config/services.example.json")
	if err != nil {
		t.Fatalf("load example manifest: %v", err)
	}
	definitions := manifest.Services
	if len(definitions) != 3 {
		t.Fatalf("expected 3 example services, got %d", len(definitions))
	}
	if len(manifest.QuickLinks) != 3 || manifest.Footer.Name == "" {
		t.Fatalf("unexpected presentation configuration: %#v", manifest)
	}
	if manifest.Site.Name != "myDashboard" || manifest.Site.TimeZone != "Europe/Zurich" {
		t.Fatalf("unexpected site configuration: %#v", manifest.Site)
	}
	for _, definition := range definitions {
		if definition.URL == "" {
			continue
		}
		parsed, err := url.Parse(definition.URL)
		if err != nil {
			t.Fatalf("service %q: unparseable url %q: %v", definition.ID, definition.URL, err)
		}
		if parsed.Scheme != "https" || parsed.Host == "" {
			t.Fatalf("service %q: url must be absolute https, got %q", definition.ID, definition.URL)
		}
	}
}

// The content security policy only allows same-origin images, so a remote icon
// would silently fall back and leak the service inventory to whoever hosts it.
func TestExampleManifestServesIconsItself(t *testing.T) {
	manifest, err := LoadManifestFile("../../config/services.example.json")
	if err != nil {
		t.Fatalf("load example manifest: %v", err)
	}
	for _, definition := range manifest.Services {
		if definition.Icon == "" {
			continue
		}
		if !strings.HasPrefix(definition.Icon, "/") {
			t.Fatalf("service %q: icon must be served locally, got %q", definition.ID, definition.Icon)
		}
	}
}

func TestManifestDefaultsSiteWhenOmitted(t *testing.T) {
	manifest, err := LoadManifest(strings.NewReader(`{` + validPresentationPrefix + `"services":[]}`))
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	if manifest.Site.Name != "myDashboard" || manifest.Site.TimeZone != "" {
		t.Fatalf("expected default site, got %#v", manifest.Site)
	}
}

func TestManifestRejectsUnknownSiteTimeZone(t *testing.T) {
	manifest := `{"site":{"timeZone":"Mars/Olympus"},` + validPresentationPrefix + `"services":[]}`
	_, err := LoadManifest(strings.NewReader(manifest))
	if err == nil || !strings.Contains(err.Error(), "unknown timeZone") {
		t.Fatalf("expected time zone validation error, got %v", err)
	}
}

func TestManifestRejectsInvalidPresentationConfiguration(t *testing.T) {
	manifest := `{` + validPresentationPrefix + `"quickLinks":[{"id":"router","name":"Router","url":"file:///etc/passwd","icon":"router"}],"footer":{"year":2026,"name":"Patrick","url":"https://example.com","message":"Made with care"},"services":[]}`
	_, err := LoadManifest(strings.NewReader(manifest))
	if err == nil || !strings.Contains(err.Error(), "quick link") {
		t.Fatalf("expected quick-link validation error, got %v", err)
	}
}

func TestManifestRejectsInvalidStartupTimeout(t *testing.T) {
	manifest := `{` + validPresentationPrefix + `"services":[{"id":"tool","name":"Tool","project":"tool","category":"tools","lifecycle":"on-demand","startupTimeoutSeconds":5}]}`
	_, err := LoadManifest(strings.NewReader(manifest))
	if err == nil || !strings.Contains(err.Error(), "startupTimeoutSeconds") {
		t.Fatalf("expected startup timeout validation error, got %v", err)
	}
}

func TestManifestRejectsDuplicateProjects(t *testing.T) {
	manifest := `{` + validPresentationPrefix + `"services":[
		{"id":"one","name":"One","project":"shared","category":"infrastructure","lifecycle":"always-on","description":"","icon":"","url":"","schedule":"","hidden":false},
		{"id":"two","name":"Two","project":"shared","category":"tools","lifecycle":"on-demand","description":"","icon":"","url":"","schedule":"","hidden":false}
	]}`
	_, err := LoadManifest(strings.NewReader(manifest))
	if err == nil || !strings.Contains(err.Error(), "duplicate project") {
		t.Fatalf("expected duplicate project error, got %v", err)
	}
}

func TestManifestRejectsUnsupportedCategory(t *testing.T) {
	manifest := `{` + validPresentationPrefix + `"services":[{"id":"one","name":"One","project":"one","category":"misc","lifecycle":"always-on"}]}`
	_, err := LoadManifest(strings.NewReader(manifest))
	if err == nil || !strings.Contains(err.Error(), "unsupported category") {
		t.Fatalf("expected unsupported category error, got %v", err)
	}
}

func TestManifestRejectsUnknownFields(t *testing.T) {
	manifest := `{` + validPresentationPrefix + `"services":[{"id":"one","name":"One","project":"one","lifecycle":"always-on","unknown":true}]}`
	_, err := LoadManifest(strings.NewReader(manifest))
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("expected unknown field error, got %v", err)
	}
}

func TestManifestRequiresValidScheduledWindow(t *testing.T) {
	manifest := `{` + validPresentationPrefix + `"services":[{"id":"worker","name":"Worker","project":"worker","category":"automation","lifecycle":"scheduled","windowStart":"5:30","windowEnd":"20:00"}]}`
	_, err := LoadManifest(strings.NewReader(manifest))
	if err == nil || !strings.Contains(err.Error(), "requires valid windowStart") {
		t.Fatalf("expected invalid schedule window error, got %v", err)
	}
}

func TestManifestRejectsWindowForAlwaysOnService(t *testing.T) {
	manifest := `{` + validPresentationPrefix + `"services":[{"id":"worker","name":"Worker","project":"worker","category":"automation","lifecycle":"always-on","windowStart":"05:30","windowEnd":"20:00"}]}`
	_, err := LoadManifest(strings.NewReader(manifest))
	if err == nil || !strings.Contains(err.Error(), "schedule windows require scheduled lifecycle") {
		t.Fatalf("expected lifecycle window error, got %v", err)
	}
}

func TestManifestRejectsUnsafeCheckURL(t *testing.T) {
	manifest := `{` + validPresentationPrefix + `"services":[{"id":"tool","name":"Tool","project":"tool","category":"tools","lifecycle":"always-on","url":"https://tool.example","checkUrl":"http://user:secret@tool:8080/"}]}`
	_, err := LoadManifest(strings.NewReader(manifest))
	if err == nil || !strings.Contains(err.Error(), "must not contain credentials") {
		t.Fatalf("expected credential-bearing checkUrl error, got %v", err)
	}
}
