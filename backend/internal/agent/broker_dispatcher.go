package agent

import (
	"context"
	"errors"
	"strings"

	"github.com/babelsuite/babelsuite/internal/logstream"
	"github.com/babelsuite/babelsuite/internal/strutil"
)

type BrokerDispatcher struct {
	backendID   string
	registry    RegistryReader
	coordinator *Coordinator
}

func NewBrokerDispatcher(backendID string, registry RegistryReader, coordinator *Coordinator) *BrokerDispatcher {
	return &BrokerDispatcher{
		backendID:   backendID,
		registry:    registry,
		coordinator: coordinator,
	}
}

func anyAgentSatisfies(registrations []Registration, devices []string) bool {
	for _, reg := range registrations {
		if capabilitiesSatisfied(reg.Capabilities, devices) {
			return true
		}
	}
	return false
}

func capabilitiesSatisfied(have, need []string) bool {
	caps := make(map[string]struct{}, len(have))
	for _, c := range have {
		caps[strings.SplitN(c, ":", 2)[0]] = struct{}{}
	}
	for _, n := range need {
		if _, ok := caps[strings.SplitN(n, ":", 2)[0]]; !ok {
			return false
		}
	}
	return true
}

func (d *BrokerDispatcher) IsAvailable(context.Context) bool {
	if d == nil || d.registry == nil {
		return false
	}
	if registry, ok := d.registry.(*Registry); ok && registry == nil {
		return false
	}
	return d.registry.IsAvailable(d.backendID)
}

func (d *BrokerDispatcher) Dispatch(ctx context.Context, request StepRequest, _ func(logstream.Line)) error {
	if d == nil || d.coordinator == nil {
		return errors.New("remote broker is not configured")
	}

	if len(request.Node.Devices) > 0 && d.registry != nil {
		if !anyAgentSatisfies(d.registry.ListRegistrations(), request.Node.Devices) {
			return errors.New("no available agent satisfies device requirements: " + strings.Join(request.Node.Devices, ", "))
		}
	}

	request.BackendID = strutil.FirstNonEmpty(request.BackendID, d.backendID)
	assignment, err := d.coordinator.Submit(request)
	if err != nil {
		return err
	}
	defer d.coordinator.Cleanup(assignment.JobID)

	return d.coordinator.Wait(ctx, assignment.JobID)
}
