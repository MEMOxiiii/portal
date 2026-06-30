package event

import "sync"

// Bus is a simple synchronous publish/subscribe event bus used to notify interested code of proxy-wide
// occurrences (players joining/quitting, servers registering, transfers completing, etc.) without requiring
// a fork of the proxy to hook into them.
type Bus struct {
	mu       sync.RWMutex
	handlers map[string][]func(any)
}

// NewBus creates an empty event bus.
func NewBus() *Bus {
	return &Bus{handlers: make(map[string][]func(any))}
}

// Subscribe registers fn to be called, with the published payload, every time an event is published under
// topic. Handlers are called synchronously and in registration order on the goroutine that calls Publish,
// so slow handlers should offload their work to their own goroutine.
func (b *Bus) Subscribe(topic string, fn func(payload any)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[topic] = append(b.handlers[topic], fn)
}

// Publish calls every handler subscribed to topic with the provided payload.
func (b *Bus) Publish(topic string, payload any) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, fn := range b.handlers[topic] {
		fn(payload)
	}
}
