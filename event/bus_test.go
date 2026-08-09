package event

import (
	"sync"
	"testing"
)

func TestBusPublishesInRegistrationOrder(t *testing.T) {
	b := NewBus()

	var order []string
	b.Subscribe("topic", func(any) { order = append(order, "first") })
	b.Subscribe("topic", func(any) { order = append(order, "second") })
	b.Publish("topic", nil)

	if len(order) != 2 || order[0] != "first" || order[1] != "second" {
		t.Errorf("handlers ran in order %v, want [first second]", order)
	}
}

func TestBusUnsubscribeStopsHandler(t *testing.T) {
	b := NewBus()

	var kept, removed int
	b.Subscribe("topic", func(any) { kept++ })
	sub := b.Subscribe("topic", func(any) { removed++ })

	b.Publish("topic", nil)
	if kept != 1 || removed != 1 {
		t.Fatalf("before unsubscribe: kept=%d removed=%d, want 1 and 1", kept, removed)
	}

	sub.Unsubscribe()
	b.Publish("topic", nil)
	if kept != 2 {
		t.Errorf("remaining handler called %d times, want 2", kept)
	}
	if removed != 1 {
		t.Errorf("unsubscribed handler called %d times, want it to stay at 1", removed)
	}
}

func TestBusUnsubscribeIsIdempotent(t *testing.T) {
	b := NewBus()

	var calls int
	first := b.Subscribe("topic", func(any) { calls++ })
	second := b.Subscribe("topic", func(any) { calls++ })

	first.Unsubscribe()
	first.Unsubscribe()

	b.Publish("topic", nil)
	if calls != 1 {
		t.Errorf("handler calls = %d, want 1: a repeated Unsubscribe must not remove another handler", calls)
	}
	second.Unsubscribe()
	b.Publish("topic", nil)
	if calls != 1 {
		t.Errorf("handler calls = %d after removing every handler, want 1", calls)
	}
}

func TestBusUnsubscribeOnNilSubscriptionIsSafe(t *testing.T) {
	var sub *Subscription
	sub.Unsubscribe()
}

func TestBusRecoversPanickingHandler(t *testing.T) {
	b := NewBus()

	var reached bool
	b.Subscribe("topic", func(any) { panic("handler exploded") })
	b.Subscribe("topic", func(any) { reached = true })

	b.Publish("topic", nil)
	if !reached {
		t.Error("a panic in one handler prevented the next from running")
	}
}

func TestBusHandlerMayUnsubscribeDuringPublish(t *testing.T) {
	b := NewBus()

	var calls int
	var sub *Subscription
	sub = b.Subscribe("topic", func(any) {
		calls++
		sub.Unsubscribe()
	})

	b.Publish("topic", nil)
	b.Publish("topic", nil)
	if calls != 1 {
		t.Errorf("handler calls = %d, want 1", calls)
	}
}

func TestBusConcurrentSubscribeAndPublish(t *testing.T) {
	b := NewBus()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			b.Subscribe("topic", func(any) {}).Unsubscribe()
		}()
		go func() {
			defer wg.Done()
			b.Publish("topic", nil)
		}()
	}
	wg.Wait()
}
