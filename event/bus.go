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
	nextID   uint64
	handlers map[string][]*subscriber
}

// subscriber pairs a registered handler with the ID used to remove it again.
type subscriber struct {
	id uint64
	fn func(any)
}

// Subscription identifies a single handler registered with Subscribe. It is used to remove that handler
// from the bus again, which is required for code with a lifetime shorter than the proxy's, such as a plugin
// that may be disabled while the proxy keeps running.
type Subscription struct {
	once  sync.Once
	bus   *Bus
	topic string
	id    uint64
}

// NewBus creates an empty event bus.
func NewBus() *Bus {
	return &Bus{handlers: make(map[string][]*subscriber)}
}

// Subscribe registers fn to be called, with the published payload, every time an event is published under
// topic. Handlers are called synchronously and in registration order on the goroutine that calls Publish,
// so slow handlers should offload their work to their own goroutine. The returned Subscription may be used
// to remove the handler again, and may be ignored by callers that subscribe for the lifetime of the proxy.
func (b *Bus) Subscribe(topic string, fn func(payload any)) *Subscription {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.nextID++
	id := b.nextID
	b.handlers[topic] = append(b.handlers[topic], &subscriber{id: id, fn: fn})
	return &Subscription{bus: b, topic: topic, id: id}
}

// Unsubscribe removes the handler the Subscription refers to from the bus, after which it is no longer
// called for newly published events. It is safe to call on a nil Subscription and to call more than once,
// and only takes effect on the first call.
func (s *Subscription) Unsubscribe() {
	if s == nil || s.bus == nil {
		return
	}
	s.once.Do(func() {
		b := s.bus
		b.mu.Lock()
		defer b.mu.Unlock()

		subs := b.handlers[s.topic]
		for i, sub := range subs {
			if sub.id != s.id {
				continue
			}
			// Copy rather than shifting in place so that a snapshot taken by a Publish still in flight
			// keeps observing the handler set it started with.
			remaining := append(subs[:i:i], subs[i+1:]...)
			if len(remaining) == 0 {
				delete(b.handlers, s.topic)
			} else {
				b.handlers[s.topic] = remaining
			}
			break
		}
	})
}

// Publish calls every handler subscribed to topic with the provided payload. Handlers are invoked outside
// of the bus's lock and each is individually recovered, so a handler that panics or calls Subscribe/Publish
// on the same bus can't crash the caller or self-deadlock the bus.
func (b *Bus) Publish(topic string, payload any) {
	b.mu.RLock()
	handlers := append([]*subscriber(nil), b.handlers[topic]...)
	b.mu.RUnlock()

	for _, sub := range handlers {
		b.call(sub.fn, payload)
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
