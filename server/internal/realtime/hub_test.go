package realtime

import "testing"

func TestHub_FansOutPerDatabase(t *testing.T) {
	h := NewHub()
	a := h.Subscribe("shopdb")
	b := h.Subscribe("ShopDB") // case-insensitive, same database
	other := h.Subscribe("otherdb")
	defer a.Close()
	defer b.Close()
	defer other.Close()

	if got := h.Subscribers("shopdb"); got != 2 {
		t.Fatalf("subscribers = %d, want 2", got)
	}

	h.Publish(Event{Type: "data", Resource: "orders", Database: "shopdb"})
	for _, sub := range []*Subscription{a, b} {
		select {
		case ev := <-sub.Events:
			if ev.Resource != "orders" || ev.At == 0 {
				t.Fatalf("unexpected event %+v", ev)
			}
		default:
			t.Fatal("subscriber of shopdb did not receive the event")
		}
	}
	select {
	case ev := <-other.Events:
		t.Fatalf("otherdb must not receive shopdb's event, got %+v", ev)
	default:
	}
}

func TestHub_CloseUnsubscribes(t *testing.T) {
	h := NewHub()
	s := h.Subscribe("db")
	s.Close()
	if got := h.Subscribers("db"); got != 0 {
		t.Fatalf("subscribers after close = %d, want 0", got)
	}
	h.Publish(Event{Database: "db"}) // must not panic or block
}

func TestHub_FullQueueIsDropped(t *testing.T) {
	h := NewHub()
	s := h.Subscribe("db")
	defer s.Close()
	// Overfill the buffer; publish must never block.
	for i := 0; i < subscriptionBuffer+10; i++ {
		h.Publish(Event{Database: "db", Resource: "t"})
	}
	if len(s.Events) != subscriptionBuffer {
		t.Fatalf("queue length = %d, want %d (extras dropped)", len(s.Events), subscriptionBuffer)
	}
}
