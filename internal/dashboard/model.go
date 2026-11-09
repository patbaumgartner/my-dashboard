package dashboard

import "time"

type Lifecycle string

type Category string

type ControlPolicy string

type ServiceAction string

const (
	LifecycleAlwaysOn   Lifecycle = "always-on"
	LifecycleScheduled  Lifecycle = "scheduled"
	LifecycleOnDemand   Lifecycle = "on-demand"
	LifecycleDiscovered Lifecycle = "discovered"

	CategoryInfrastructure Category = "infrastructure"
	CategoryMonitoring     Category = "monitoring"
	CategoryMedia          Category = "media"
	CategoryTools          Category = "tools"
	CategoryAutomation     Category = "automation"
	CategoryDiscovered     Category = "discovered"

	ControlNone  ControlPolicy = ""
	ControlPower ControlPolicy = "power"

	ActionStart   ServiceAction = "start"
	ActionStop    ServiceAction = "stop"
	ActionRestart ServiceAction = "restart"
)

type Manifest struct {
	Site             SiteDefinition              `json:"site"`
	LifecycleFilters []LifecycleFilterDefinition `json:"lifecycleFilters"`
	Categories       []CategoryDefinition        `json:"categories"`
	QuickLinks       []QuickLinkDefinition       `json:"quickLinks"`
	Footer           FooterDefinition            `json:"footer"`
	Services         []ServiceDefinition         `json:"services"`
}

// SiteDefinition brands the dashboard. Every field is optional: Name defaults
// to "myDashboard" and an empty TimeZone means the browser's local zone.
type SiteDefinition struct {
	Name        string `json:"name"`
	Subtitle    string `json:"subtitle"`
	Description string `json:"description"`
	TimeZone    string `json:"timeZone"`
}

type LifecycleFilterDefinition struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	ShortLabel string `json:"shortLabel"`
}

type CategoryDefinition struct {
	ID          Category `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
}

type QuickLinkDefinition struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	URL         string `json:"url"`
	Icon        string `json:"icon"`
}

type FooterDefinition struct {
	Year    int    `json:"year"`
	Name    string `json:"name"`
	URL     string `json:"url"`
	Message string `json:"message"`
}

type ServiceDefinition struct {
	ID                    string        `json:"id"`
	Name                  string        `json:"name"`
	Description           string        `json:"description"`
	Icon                  string        `json:"icon"`
	URL                   string        `json:"url"`
	CheckURL              string        `json:"checkUrl"`
	Project               string        `json:"project"`
	Category              Category      `json:"category"`
	Lifecycle             Lifecycle     `json:"lifecycle"`
	Control               ControlPolicy `json:"control"`
	Schedule              string        `json:"schedule"`
	WindowStart           string        `json:"windowStart"`
	WindowEnd             string        `json:"windowEnd"`
	StartupTimeoutSeconds int           `json:"startupTimeoutSeconds"`
	Hidden                bool          `json:"hidden"`
}

type DockerContainer struct {
	ID     string            `json:"Id"`
	Names  []string          `json:"Names"`
	Image  string            `json:"Image"`
	State  string            `json:"State"`
	Status string            `json:"Status"`
	Labels map[string]string `json:"Labels"`
}

type ContainerState struct {
	Name   string `json:"name"`
	Image  string `json:"image"`
	State  string `json:"state"`
	Status string `json:"status"`
	Health string `json:"health"`
}

type Service struct {
	ID                    string           `json:"id"`
	Name                  string           `json:"name"`
	Description           string           `json:"description"`
	Icon                  string           `json:"icon"`
	URL                   string           `json:"url"`
	CheckURL              string           `json:"-"`
	Project               string           `json:"project"`
	Category              Category         `json:"category"`
	Lifecycle             Lifecycle        `json:"lifecycle"`
	Schedule              string           `json:"schedule"`
	WindowStart           string           `json:"windowStart"`
	WindowEnd             string           `json:"windowEnd"`
	StartupTimeoutSeconds int              `json:"startupTimeoutSeconds"`
	State                 string           `json:"state"`
	Status                string           `json:"status"`
	Available             bool             `json:"available"`
	Availability          string           `json:"availability"`
	ControlsLocked        bool             `json:"controlsLocked"`
	AvailableActions      []ServiceAction  `json:"availableActions"`
	Containers            []ContainerState `json:"containers"`
}

type ServicesResponse struct {
	Site             SiteDefinition              `json:"site"`
	LifecycleFilters []LifecycleFilterDefinition `json:"lifecycleFilters"`
	Categories       []CategoryDefinition        `json:"categories"`
	QuickLinks       []QuickLinkDefinition       `json:"quickLinks"`
	Footer           FooterDefinition            `json:"footer"`
	Services         []Service                   `json:"services"`
	UpdatedAt        time.Time                   `json:"updatedAt"`
	DockerConnected  bool                        `json:"dockerConnected"`
	Error            string                      `json:"error,omitempty"`
}

type ActionResult struct {
	ServiceID string        `json:"serviceId"`
	Action    ServiceAction `json:"action"`
	Container string        `json:"container"`
}

type AvailabilityResponse struct {
	ServiceID             string `json:"serviceId"`
	Name                  string `json:"name"`
	URL                   string `json:"url"`
	State                 string `json:"state"`
	Available             bool   `json:"available"`
	Availability          string `json:"availability"`
	StartupTimeoutSeconds int    `json:"startupTimeoutSeconds"`
}
