package event

import (
	"log"
	"sync"
)

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

// Publish calls every handler subscribed to topic with the provided payload. Handlers are invoked outside
// of the bus's lock and each is individually recovered, so a handler that panics or calls Subscribe/Publish
// on the same bus can't crash the caller or self-deadlock the bus.
func (b *Bus) Publish(topic string, payload any) {
	b.mu.RLock()
	handlers := append([]func(any){}, b.handlers[topic]...)
	b.mu.RUnlock()

	for _, fn := range handlers {
		b.call(fn, payload)
	}
}

func (b *Bus) call(fn func(payload any), payload any) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("event: handler panicked: %v", r)
		}
	}()
	fn(payload)
}
