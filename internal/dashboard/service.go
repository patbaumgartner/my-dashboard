package dashboard

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

const composeProjectLabel = "com.docker.compose.project"

type ServiceReader struct {
	site             SiteDefinition
	definitions      []ServiceDefinition
	lifecycleFilters []LifecycleFilterDefinition
	categories       []CategoryDefinition
	quickLinks       []QuickLinkDefinition
	footer           FooterDefinition
	source           ContainerSource
	availability     AvailabilityChecker
	now              func() time.Time
}

func NewServiceReader(
	manifest Manifest,
	source ContainerSource,
	availability AvailabilityChecker,
) *ServiceReader {
	return &ServiceReader{
		site:             manifest.Site,
		definitions:      append([]ServiceDefinition(nil), manifest.Services...),
		lifecycleFilters: append([]LifecycleFilterDefinition(nil), manifest.LifecycleFilters...),
		categories:       append([]CategoryDefinition(nil), manifest.Categories...),
		quickLinks:       append([]QuickLinkDefinition(nil), manifest.QuickLinks...),
		footer:           manifest.Footer,
		source:           source,
		availability:     availability,
		now:              time.Now,
	}
}

func (reader *ServiceReader) Read(ctx context.Context) ServicesResponse {
	response := ServicesResponse{
		Site:             reader.site,
		LifecycleFilters: append([]LifecycleFilterDefinition(nil), reader.lifecycleFilters...),
		Categories:       append([]CategoryDefinition(nil), reader.categories...),
		QuickLinks:       append([]QuickLinkDefinition(nil), reader.quickLinks...),
		Footer:           reader.footer,
		UpdatedAt:        reader.now().UTC(),
	}
	containers, err := reader.source.ListContainers(ctx)
	if err != nil {
		response.Error = "Docker status is temporarily unavailable"
		response.Services = reader.configuredServices(nil, false)
		return response
	}

	response.DockerConnected = true
	response.Services = reader.configuredServices(containers, true)
	checkContext, cancel := context.WithTimeout(ctx, availabilityBudget)
	defer cancel()
	reader.checkAvailability(checkContext, response.Services)
	return response
}

// availabilityBudget bounds the route checks of one list request well below the
// HTTP server write timeout, so a few hanging services cannot stall the page.
const availabilityBudget = 10 * time.Second

func (reader *ServiceReader) CheckService(ctx context.Context, serviceID string) (AvailabilityResponse, error) {
	for _, definition := range reader.definitions {
		if definition.ID != serviceID || definition.Hidden {
			continue
		}
		response := AvailabilityResponse{
			ServiceID:             definition.ID,
			Name:                  definition.Name,
			URL:                   definition.URL,
			State:                 "unknown",
			Availability:          "No web interface",
			StartupTimeoutSeconds: definition.StartupTimeoutSeconds,
		}
		if containers, err := reader.source.ListContainers(ctx); err == nil {
			state := serviceFrom(definition, containersForProject(containers, definition.Project), true)
			response.State = state.State
		}
		if definition.URL == "" {
			return response, nil
		}
		if response.State == "stopped" {
			response.Availability = "Service not running"
			return response, nil
		}
		if reader.availability == nil {
			response.Availability = "Status check unavailable"
			return response, nil
		}
		response.Available, response.Availability = reader.checkWebAvailability(ctx, definition.URL, definition.CheckURL)
		return response, nil
	}
	return AvailabilityResponse{}, ErrServiceNotFound
}

func (reader *ServiceReader) checkAvailability(ctx context.Context, services []Service) {
	if reader.availability == nil {
		return
	}
	var waitGroup sync.WaitGroup
	semaphore := make(chan struct{}, 6)
	for index := range services {
		service := &services[index]
		if service.URL == "" {
			service.Availability = "No web interface"
			continue
		}
		if service.State == "stopped" || service.State == "unknown" {
			service.Availability = "Service not running"
			continue
		}
		waitGroup.Add(1)
		go func(target *Service) {
			defer waitGroup.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				target.Availability = "Check cancelled"
				return
			}
			target.Available, target.Availability = reader.checkWebAvailability(ctx, target.URL, target.CheckURL)
			if !target.Available && target.State != "degraded" {
				target.State = "degraded"
				target.Status = "Containers running; " + target.Availability
			}
		}(service)
	}
	waitGroup.Wait()
}

// checkWebAvailability requires both the backend check and the browser-facing
// route. Both probes run concurrently; a backend failure is reported first.
func (reader *ServiceReader) checkWebAvailability(ctx context.Context, publicURL, checkURL string) (bool, string) {
	if checkURL == "" || checkURL == publicURL {
		return reader.availability.Check(ctx, publicURL)
	}
	type result struct {
		available bool
		note      string
	}
	backend := make(chan result, 1)
	go func() {
		available, note := reader.availability.Check(ctx, checkURL)
		backend <- result{available, note}
	}()
	routeAvailable, routeNote := reader.availability.Check(ctx, publicURL)
	backendResult := <-backend
	if !backendResult.available {
		return false, "Backend: " + backendResult.note
	}
	return routeAvailable, routeNote
}

func (reader *ServiceReader) configuredServices(containers []DockerContainer, connected bool) []Service {
	byProject := make(map[string][]DockerContainer)
	for _, container := range containers {
		project := strings.TrimSpace(container.Labels[composeProjectLabel])
		if project != "" {
			byProject[project] = append(byProject[project], container)
		}
	}

	knownProjects := make(map[string]struct{}, len(reader.definitions))
	usedIDs := make(map[string]struct{}, len(reader.definitions)+len(byProject))
	services := make([]Service, 0, len(reader.definitions)+len(byProject))
	for _, definition := range reader.definitions {
		knownProjects[definition.Project] = struct{}{}
		usedIDs[definition.ID] = struct{}{}
		if definition.Hidden {
			continue
		}
		services = append(services, serviceFrom(definition, byProject[definition.Project], connected))
	}

	unknownProjects := make([]string, 0, len(byProject))
	for project := range byProject {
		if _, known := knownProjects[project]; !known {
			unknownProjects = append(unknownProjects, project)
		}
	}
	// Sorted so collision suffixes stay stable across requests.
	sort.Strings(unknownProjects)

	discovered := make([]Service, 0, len(unknownProjects))
	for _, project := range unknownProjects {
		projectContainers := byProject[project]
		id := "discovered-" + identifier(project)
		for suffix := 2; ; suffix++ {
			if _, taken := usedIDs[id]; !taken {
				break
			}
			id = fmt.Sprintf("discovered-%s-%d", identifier(project), suffix)
		}
		usedIDs[id] = struct{}{}
		definition := ServiceDefinition{
			ID:          id,
			Name:        displayName(project),
			Description: "Compose project discovered from Docker",
			Project:     project,
			Category:    CategoryDiscovered,
			Lifecycle:   LifecycleDiscovered,
		}
		discovered = append(discovered, serviceFrom(definition, projectContainers, true))
	}
	sort.SliceStable(discovered, func(left, right int) bool {
		return discovered[left].Name < discovered[right].Name
	})

	return append(services, discovered...)
}

func serviceFrom(definition ServiceDefinition, containers []DockerContainer, connected bool) Service {
	service := Service{
		ID:                    definition.ID,
		Name:                  definition.Name,
		Description:           definition.Description,
		Icon:                  definition.Icon,
		URL:                   definition.URL,
		CheckURL:              definition.CheckURL,
		Project:               definition.Project,
		Category:              definition.Category,
		Lifecycle:             definition.Lifecycle,
		Schedule:              definition.Schedule,
		WindowStart:           definition.WindowStart,
		WindowEnd:             definition.WindowEnd,
		StartupTimeoutSeconds: definition.StartupTimeoutSeconds,
		State:                 "unknown",
		Status:                "Status unavailable",
		Availability:          "Status unavailable",
		ControlsLocked:        true,
		AvailableActions:      []ServiceAction{},
		Containers:            make([]ContainerState, 0, len(containers)),
	}
	if !connected {
		return service
	}

	for _, container := range containers {
		service.Containers = append(service.Containers, ContainerState{
			Name:   containerName(container),
			Image:  container.Image,
			State:  container.State,
			Status: container.Status,
			Health: containerHealth(container.Status),
		})
	}
	sort.Slice(service.Containers, func(left, right int) bool {
		return service.Containers[left].Name < service.Containers[right].Name
	})

	service.State, service.Status = projectState(service.Containers)
	service.AvailableActions = availableActions(definition.Control, service.State, len(service.Containers))
	service.ControlsLocked = len(service.AvailableActions) == 0
	return service
}

func availableActions(control ControlPolicy, state string, containerCount int) []ServiceAction {
	if control != ControlPower || containerCount == 0 {
		return []ServiceAction{}
	}
	if state == "stopped" {
		return []ServiceAction{ActionStart}
	}
	return []ServiceAction{ActionStop, ActionRestart}
}

func projectState(containers []ContainerState) (string, string) {
	if len(containers) == 0 {
		return "stopped", "Not created or stopped"
	}

	running := 0
	healthy := 0
	unhealthy := 0
	for _, container := range containers {
		if container.State == "running" {
			running++
		}
		switch container.Health {
		case "healthy":
			healthy++
		case "unhealthy":
			unhealthy++
		}
	}

	if unhealthy > 0 {
		return "degraded", fmt.Sprintf("%d unhealthy", unhealthy)
	}
	if running == 0 {
		return "stopped", "All containers stopped"
	}
	if running < len(containers) {
		return "degraded", fmt.Sprintf("%d of %d running", running, len(containers))
	}
	if healthy > 0 {
		return "healthy", fmt.Sprintf("%d running, %d healthy", running, healthy)
	}
	return "running", fmt.Sprintf("%d running", running)
}

func containerName(container DockerContainer) string {
	if len(container.Names) == 0 {
		return container.ID
	}
	return strings.TrimPrefix(container.Names[0], "/")
}

func containerHealth(status string) string {
	lower := strings.ToLower(status)
	switch {
	case strings.Contains(lower, "(unhealthy)"):
		return "unhealthy"
	case strings.Contains(lower, "(healthy)"):
		return "healthy"
	case strings.Contains(lower, "health: starting") || strings.Contains(lower, "(health: starting)"):
		return "starting"
	default:
		return ""
	}
}

func displayName(project string) string {
	parts := strings.FieldsFunc(project, func(character rune) bool {
		return character == '-' || character == '_'
	})
	for index := range parts {
		if parts[index] != "" {
			parts[index] = strings.ToUpper(parts[index][:1]) + parts[index][1:]
		}
	}
	return strings.Join(parts, " ")
}

func identifier(value string) string {
	var result strings.Builder
	for _, character := range strings.ToLower(value) {
		switch {
		case character >= 'a' && character <= 'z':
			result.WriteRune(character)
		case character >= '0' && character <= '9':
			result.WriteRune(character)
		case result.Len() > 0 && !strings.HasSuffix(result.String(), "-"):
			result.WriteByte('-')
		}
	}
	return strings.Trim(result.String(), "-")
}
