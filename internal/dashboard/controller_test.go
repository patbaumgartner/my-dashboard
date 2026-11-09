package dashboard

import (
	"context"
	"errors"
	"testing"
)

type recordingOperator struct {
	containers []DockerContainer
	actions    []string
}

func (operator *recordingOperator) ListContainers(context.Context) ([]DockerContainer, error) {
	return operator.containers, nil
}

func (operator *recordingOperator) StartContainer(_ context.Context, id string) error {
	operator.actions = append(operator.actions, "start:"+id)
	return nil
}

func (operator *recordingOperator) StopContainer(_ context.Context, id string) error {
	operator.actions = append(operator.actions, "stop:"+id)
	return nil
}

func (operator *recordingOperator) RestartContainer(_ context.Context, id string) error {
	operator.actions = append(operator.actions, "restart:"+id)
	return nil
}

func TestServiceControllerTargetsOnlyAllowlistedProjectContainer(t *testing.T) {
	operator := &recordingOperator{containers: []DockerContainer{
		{ID: "allowed-id", Names: []string{"/allowed"}, State: "exited", Labels: map[string]string{composeProjectLabel: "allowed-project"}},
		{ID: "other-id", Names: []string{"/other"}, State: "exited", Labels: map[string]string{composeProjectLabel: "other-project"}},
	}}
	controller := NewServiceController([]ServiceDefinition{
		{ID: "allowed", Name: "Allowed", Project: "allowed-project", Lifecycle: LifecycleOnDemand, Control: ControlPower},
		{ID: "readonly", Name: "Readonly", Project: "other-project", Lifecycle: LifecycleAlwaysOn},
	}, operator)

	result, err := controller.Execute(context.Background(), "allowed", ActionStart)
	if err != nil {
		t.Fatalf("execute action: %v", err)
	}
	if result.Container != "allowed" || len(operator.actions) != 1 || operator.actions[0] != "start:allowed-id" {
		t.Fatalf("unexpected operation: result=%#v actions=%#v", result, operator.actions)
	}

	if _, err := controller.Execute(context.Background(), "readonly", ActionStart); !errors.Is(err, ErrActionForbidden) {
		t.Fatalf("expected readonly service rejection, got %v", err)
	}
	if _, err := controller.Execute(context.Background(), "missing", ActionStart); !errors.Is(err, ErrServiceNotFound) {
		t.Fatalf("expected unknown service rejection, got %v", err)
	}
	if len(operator.actions) != 1 {
		t.Fatalf("rejected actions reached Docker: %#v", operator.actions)
	}
}

func TestServiceControllerRejectsMultiContainerPowerProject(t *testing.T) {
	operator := &recordingOperator{containers: []DockerContainer{
		{ID: "app", State: "running", Labels: map[string]string{composeProjectLabel: "stack"}},
		{ID: "db", State: "running", Labels: map[string]string{composeProjectLabel: "stack"}},
	}}
	controller := NewServiceController([]ServiceDefinition{{
		ID: "stack", Name: "Stack", Project: "stack", Lifecycle: LifecycleOnDemand, Control: ControlPower,
	}}, operator)

	if _, err := controller.Execute(context.Background(), "stack", ActionStop); !errors.Is(err, ErrComposeRequired) {
		t.Fatalf("expected Compose boundary rejection, got %v", err)
	}
	if len(operator.actions) != 0 {
		t.Fatalf("rejected multi-container action reached Docker: %#v", operator.actions)
	}
}
