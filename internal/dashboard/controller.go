package dashboard

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

var (
	ErrServiceNotFound = errors.New("service not found")
	ErrActionForbidden = errors.New("action is not allowed")
	ErrServiceBusy     = errors.New("service operation already in progress")
	ErrNoContainers    = errors.New("service has no existing containers")
	ErrComposeRequired = errors.New("multi-container projects require Compose-aware control")
)

type ServiceController struct {
	definitions map[string]ServiceDefinition
	operator    ContainerOperator
	locks       map[string]*sync.Mutex
}

func NewServiceController(definitions []ServiceDefinition, operator ContainerOperator) *ServiceController {
	controller := &ServiceController{
		definitions: make(map[string]ServiceDefinition, len(definitions)),
		operator:    operator,
		locks:       make(map[string]*sync.Mutex, len(definitions)),
	}
	for _, definition := range definitions {
		controller.definitions[definition.ID] = definition
		controller.locks[definition.ID] = &sync.Mutex{}
	}
	return controller
}

func (controller *ServiceController) Execute(
	ctx context.Context,
	serviceID string,
	action ServiceAction,
) (ActionResult, error) {
	definition, exists := controller.definitions[serviceID]
	if !exists || definition.Hidden {
		return ActionResult{}, ErrServiceNotFound
	}
	if definition.Control != ControlPower || !validAction(action) {
		return ActionResult{}, ErrActionForbidden
	}

	operationLock := controller.locks[serviceID]
	if !operationLock.TryLock() {
		return ActionResult{}, ErrServiceBusy
	}
	defer operationLock.Unlock()

	containers, err := controller.operator.ListContainers(ctx)
	if err != nil {
		return ActionResult{}, fmt.Errorf("read Docker state: %w", err)
	}
	projectContainers := containersForProject(containers, definition.Project)
	if len(projectContainers) == 0 {
		return ActionResult{}, ErrNoContainers
	}
	if len(projectContainers) != 1 {
		return ActionResult{}, ErrComposeRequired
	}

	container := projectContainers[0]
	state := serviceFrom(definition, projectContainers, true)
	if !containsAction(state.AvailableActions, action) {
		return ActionResult{}, ErrActionForbidden
	}

	switch action {
	case ActionStart:
		err = controller.operator.StartContainer(ctx, container.ID)
	case ActionStop:
		err = controller.operator.StopContainer(ctx, container.ID)
	case ActionRestart:
		err = controller.operator.RestartContainer(ctx, container.ID)
	}
	if err != nil {
		return ActionResult{}, fmt.Errorf("%s service %q: %w", action, serviceID, err)
	}

	return ActionResult{
		ServiceID: serviceID,
		Action:    action,
		Container: containerName(container),
	}, nil
}

func validAction(action ServiceAction) bool {
	return action == ActionStart || action == ActionStop || action == ActionRestart
}

func containsAction(actions []ServiceAction, expected ServiceAction) bool {
	for _, action := range actions {
		if action == expected {
			return true
		}
	}
	return false
}

func containersForProject(containers []DockerContainer, project string) []DockerContainer {
	result := make([]DockerContainer, 0, 1)
	for _, container := range containers {
		if container.Labels[composeProjectLabel] == project {
			result = append(result, container)
		}
	}
	return result
}
