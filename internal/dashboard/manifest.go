package dashboard

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
	// The distroless runtime image has no zoneinfo database.
	_ "time/tzdata"
)

var clockTimePattern = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

func LoadManifestFile(path string) (Manifest, error) {
	file, err := os.Open(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("open service manifest: %w", err)
	}
	defer file.Close()

	return LoadManifest(file)
}

func LoadManifest(reader io.Reader) (Manifest, error) {
	decoder := json.NewDecoder(io.LimitReader(reader, 2<<20))
	decoder.DisallowUnknownFields()

	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode service manifest: %w", err)
	}

	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return Manifest{}, errors.New("decode service manifest: trailing JSON content")
	}

	site, err := normalizeSite(manifest.Site)
	if err != nil {
		return Manifest{}, err
	}
	manifest.Site = site

	if err := validatePresentation(manifest); err != nil {
		return Manifest{}, err
	}

	seenIDs := make(map[string]struct{}, len(manifest.Services))
	seenProjects := make(map[string]struct{}, len(manifest.Services))
	for index, service := range manifest.Services {
		if service.ID == "" || service.Name == "" || service.Project == "" {
			return Manifest{}, fmt.Errorf("service %d: id, name and project are required", index)
		}
		if !validLifecycle(service.Lifecycle) {
			return Manifest{}, fmt.Errorf("service %q: unsupported lifecycle %q", service.ID, service.Lifecycle)
		}
		if !validCategory(service.Category) {
			return Manifest{}, fmt.Errorf("service %q: unsupported category %q", service.ID, service.Category)
		}
		if !validControl(service.Control) {
			return Manifest{}, fmt.Errorf("service %q: unsupported control policy %q", service.ID, service.Control)
		}
		if service.Control == ControlPower && service.Lifecycle != LifecycleOnDemand {
			return Manifest{}, fmt.Errorf("service %q: power control requires on-demand lifecycle", service.ID)
		}
		if service.CheckURL != "" {
			if service.URL == "" {
				return Manifest{}, fmt.Errorf("service %q: checkUrl requires a browser url", service.ID)
			}
			if err := validateHTTPURL(service.CheckURL); err != nil {
				return Manifest{}, fmt.Errorf("service %q: invalid checkUrl: %w", service.ID, err)
			}
		}
		if service.Lifecycle == LifecycleScheduled {
			if !clockTimePattern.MatchString(service.WindowStart) || !clockTimePattern.MatchString(service.WindowEnd) {
				return Manifest{}, fmt.Errorf("service %q: scheduled lifecycle requires valid windowStart and windowEnd", service.ID)
			}
		} else if service.WindowStart != "" || service.WindowEnd != "" {
			return Manifest{}, fmt.Errorf("service %q: schedule windows require scheduled lifecycle", service.ID)
		}
		if service.StartupTimeoutSeconds != 0 && (service.StartupTimeoutSeconds < 10 || service.StartupTimeoutSeconds > 600) {
			return Manifest{}, fmt.Errorf("service %q: startupTimeoutSeconds must be between 10 and 600", service.ID)
		}
		if _, exists := seenIDs[service.ID]; exists {
			return Manifest{}, fmt.Errorf("service %q: duplicate id", service.ID)
		}
		if _, exists := seenProjects[service.Project]; exists {
			return Manifest{}, fmt.Errorf("service %q: duplicate project %q", service.ID, service.Project)
		}
		seenIDs[service.ID] = struct{}{}
		seenProjects[service.Project] = struct{}{}
	}

	return manifest, nil
}

const defaultSiteName = "myDashboard"

func normalizeSite(site SiteDefinition) (SiteDefinition, error) {
	site.Name = strings.TrimSpace(site.Name)
	site.Subtitle = strings.TrimSpace(site.Subtitle)
	site.Description = strings.TrimSpace(site.Description)
	site.TimeZone = strings.TrimSpace(site.TimeZone)
	if site.Name == "" {
		site.Name = defaultSiteName
	}
	if len(site.Name) > 60 || len(site.Subtitle) > 120 || len(site.Description) > 300 {
		return SiteDefinition{}, errors.New("site: name, subtitle or description is too long")
	}
	// The browser formats schedule windows in this zone, and Intl throws on an
	// unknown name, so reject it here instead of breaking the page at runtime.
	if site.TimeZone != "" && site.TimeZone != "Local" {
		if _, err := time.LoadLocation(site.TimeZone); err != nil {
			return SiteDefinition{}, fmt.Errorf("site: unknown timeZone %q", site.TimeZone)
		}
	} else {
		site.TimeZone = ""
	}
	return site, nil
}

func validatePresentation(manifest Manifest) error {
	if len(manifest.LifecycleFilters) == 0 {
		return errors.New("lifecycleFilters must not be empty")
	}
	seenFilters := make(map[string]struct{}, len(manifest.LifecycleFilters))
	for index, filter := range manifest.LifecycleFilters {
		if filter.ID == "" || filter.Label == "" || filter.ShortLabel == "" {
			return fmt.Errorf("lifecycle filter %d: id, label and shortLabel are required", index)
		}
		if filter.ID != "all" && !validLifecycle(Lifecycle(filter.ID)) {
			return fmt.Errorf("lifecycle filter %q: unsupported id", filter.ID)
		}
		if _, exists := seenFilters[filter.ID]; exists {
			return fmt.Errorf("lifecycle filter %q: duplicate id", filter.ID)
		}
		seenFilters[filter.ID] = struct{}{}
	}

	if len(manifest.Categories) == 0 {
		return errors.New("categories must not be empty")
	}
	seenCategories := make(map[Category]struct{}, len(manifest.Categories))
	for index, category := range manifest.Categories {
		if !validCategory(category.ID) || category.Title == "" || category.Description == "" {
			return fmt.Errorf("category %d: valid id, title and description are required", index)
		}
		if _, exists := seenCategories[category.ID]; exists {
			return fmt.Errorf("category %q: duplicate id", category.ID)
		}
		seenCategories[category.ID] = struct{}{}
	}

	seen := make(map[string]struct{}, len(manifest.QuickLinks))
	for index, link := range manifest.QuickLinks {
		if link.ID == "" || link.Name == "" || link.URL == "" || link.Icon == "" {
			return fmt.Errorf("quick link %d: id, name, url and icon are required", index)
		}
		if _, exists := seen[link.ID]; exists {
			return fmt.Errorf("quick link %q: duplicate id", link.ID)
		}
		if err := validateHTTPURL(link.URL); err != nil {
			return fmt.Errorf("quick link %q: invalid url: %w", link.ID, err)
		}
		seen[link.ID] = struct{}{}
	}
	if manifest.Footer != (FooterDefinition{}) {
		if manifest.Footer.Year < 2000 || manifest.Footer.Name == "" || manifest.Footer.Message == "" {
			return errors.New("footer: year, name and message are required")
		}
		if err := validateHTTPURL(manifest.Footer.URL); err != nil {
			return fmt.Errorf("footer: invalid url: %w", err)
		}
	}
	return nil
}

func validateHTTPURL(value string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" {
		return errors.New("must be an absolute URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("must use http or https")
	}
	if parsed.User != nil {
		return errors.New("must not contain credentials")
	}
	return nil
}

func validControl(control ControlPolicy) bool {
	return control == ControlNone || control == ControlPower
}

func validCategory(category Category) bool {
	switch category {
	case CategoryInfrastructure, CategoryMonitoring, CategoryMedia, CategoryTools, CategoryAutomation, CategoryDiscovered:
		return true
	default:
		return false
	}
}

func validLifecycle(lifecycle Lifecycle) bool {
	switch lifecycle {
	case LifecycleAlwaysOn, LifecycleScheduled, LifecycleOnDemand, LifecycleDiscovered:
		return true
	default:
		return false
	}
}
