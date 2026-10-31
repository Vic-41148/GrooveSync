package api

import (
	"encoding/json"
	"sync"
)

// StreamEvent is one server-sent event sent to connected devices.
type StreamEvent struct {
	Event string
	Data  json.RawMessage
}

// EventBroadcaster fans server changes out to connected devices. A slow
// subscriber drops events rather than blocking the writer.
type EventBroadcaster interface {
	Subscribe() (<-chan StreamEvent, func())
	Publish(string, any)
}

// EventStream is an in-process broadcaster for the SSE endpoint.
type EventStream struct {
	mu   sync.Mutex
	next int
	subs map[int]chan StreamEvent
}

// NewEventStream creates an empty broadcaster.
func NewEventStream() *EventStream {
	return &EventStream{subs: make(map[int]chan StreamEvent)}
}

// Subscribe returns a channel of events and a function that releases it.
func (s *EventStream) Subscribe() (<-chan StreamEvent, func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.next
	s.next++
	ch := make(chan StreamEvent, 16)
	s.subs[id] = ch
	return ch, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if existing, ok := s.subs[id]; ok {
			delete(s.subs, id)
			close(existing)
		}
	}
}

// Publish sends an event to every current subscriber.
func (s *EventStream) Publish(event string, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	item := StreamEvent{Event: event, Data: data}
	for _, ch := range s.subs {
		select {
		case ch <- item:
		default:
		}
	}
}
