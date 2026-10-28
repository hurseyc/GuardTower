// Package events is a tiny in-process pub/sub used to push live updates
// (instance status, new query-log rows, rollout progress) to SSE clients.
package events

import "sync"

// Event is a typed message.
type Event struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

// Bus fans events out to subscribers. Slow subscribers drop events rather
// than block publishers.
type Bus struct {
	mu   sync.RWMutex
	subs map[chan Event]struct{}
}

// NewBus creates a bus.
func NewBus() *Bus { return &Bus{subs: map[chan Event]struct{}{}} }

// Subscribe returns a channel of events and a cancel func.
func (b *Bus) Subscribe(buffer int) (<-chan Event, func()) {
	ch := make(chan Event, buffer)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			b.mu.Lock()
			delete(b.subs, ch)
			b.mu.Unlock()
			close(ch)
		})
	}
}

// Publish sends an event to every subscriber without blocking.
func (b *Bus) Publish(typ string, data any) {
	e := Event{Type: typ, Data: data}
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.subs {
		select {
		case ch <- e:
		default:
		}
	}
}
