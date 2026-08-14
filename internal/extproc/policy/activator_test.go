package policy

import (
	"context"
	"sync"
	"testing"
)

func TestNewActivatorRequiresRepository(t *testing.T) {
	if _, err := NewActivator(nil, &recordingPublisher{}); err == nil {
		t.Fatal("NewActivator(nil) error = nil")
	}
}

func TestNewActivatorRequiresPublisher(t *testing.T) {
	if _, err := NewActivator(&stubRepository{}, nil); err == nil {
		t.Fatal("NewActivator(repository, nil) error = nil")
	}
}

func TestActivatorRepublishActivationUsesCurrentActiveVersion(t *testing.T) {
	version := 7
	publisher := &recordingPublisher{}
	activator, err := NewActivator(activeSnapshotRepository{snapshot: PolicySnapshot{ID: 12, PolicyName: "inline-policy", Version: &version, Status: StatusActive}}, publisher)
	if err != nil {
		t.Fatal(err)
	}
	if err := activator.RepublishActivation(context.Background(), "inline-policy", nil); err != nil {
		t.Fatalf("RepublishActivation() error = %v", err)
	}
	events := publisher.Events()
	if len(events) != 1 || events[0] != (ActivationEvent{PolicyID: "inline-policy", Version: 7}) {
		t.Fatalf("events = %+v", events)
	}
}

type stubRepository struct{ Repository }

type activeSnapshotRepository struct {
	stubRepository
	snapshot PolicySnapshot
}

func (r activeSnapshotRepository) ActiveSnapshot(context.Context, string, *string) (PolicySnapshot, error) {
	return r.snapshot, nil
}

type recordingPublisher struct {
	mu     sync.Mutex
	events []ActivationEvent
	err    error
}

func (p *recordingPublisher) PublishActivation(_ context.Context, event ActivationEvent) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, event)
	return p.err
}

func (p *recordingPublisher) Events() []ActivationEvent {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]ActivationEvent(nil), p.events...)
}
