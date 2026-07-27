package litime

import (
	"sync"
	"testing"
	"time"
)

func testManager(t *testing.T, bufferSize int) *Manager {
	t.Helper()

	m, err := NewManager(ManagerOptions{
		Batteries:      []Battery{{ID: "garage", Match: MatchName, Value: "battery"}},
		ScrapeInterval: time.Second,
		StaleTimeout:   10 * time.Second,
		BufferSize:     bufferSize,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	return m
}

func TestNewManagerRejectsStaleTimeoutBelowScrapeInterval(t *testing.T) {
	t.Parallel()

	// A stale timeout under the scrape interval would declare every battery
	// dead before it had a chance to answer, reconnecting in a tight loop.
	_, err := NewManager(ManagerOptions{
		Batteries:      []Battery{{ID: "garage", Match: MatchName, Value: "battery"}},
		ScrapeInterval: 30 * time.Second,
		StaleTimeout:   10 * time.Second,
	})
	if err == nil {
		t.Fatal("expected an error when stale timeout is below the scrape interval")
	}
}

func TestNewManagerRequiresBatteries(t *testing.T) {
	t.Parallel()

	if _, err := NewManager(ManagerOptions{}); err == nil {
		t.Fatal("expected an error when no batteries are configured")
	}
}

func TestPublishDropsWhenQueueFull(t *testing.T) {
	t.Parallel()

	m := testManager(t, 1)

	if got := m.publish(Reading{BatteryID: "garage"}); got != published {
		t.Fatalf("first publish = %v, want published", got)
	}

	// Nothing is draining, so the queue is now full. Dropping is required
	// rather than blocking: publish runs on the Bluetooth dispatch goroutine.
	if got := m.publish(Reading{BatteryID: "garage"}); got != droppedQueueFull {
		t.Errorf("second publish = %v, want droppedQueueFull", got)
	}
}

func TestPublishAfterCloseDoesNotPanic(t *testing.T) {
	t.Parallel()

	m := testManager(t, 4)
	m.closeReadings()

	// The Bluetooth stack's notification goroutine outlives Disconnect, so a
	// reading can arrive after the stream has been closed. Sending it to the
	// closed channel would panic and take the process down at shutdown.
	if got := m.publish(Reading{BatteryID: "garage"}); got != droppedShuttingDown {
		t.Errorf("publish after close = %v, want droppedShuttingDown", got)
	}
}

// TestPublishRacesWithClose is meaningful under -race: it drives concurrent
// publishes against a close, which is exactly the shutdown ordering that a
// late-arriving notification produces.
func TestPublishRacesWithClose(t *testing.T) {
	t.Parallel()

	for range 50 {
		m := testManager(t, 8)

		var wg sync.WaitGroup
		start := make(chan struct{})

		for range 4 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				for range 25 {
					m.publish(Reading{BatteryID: "garage"})
				}
			}()
		}

		// Drain so the queue does not simply fill and make every publish a
		// no-op that never reaches the channel send.
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range m.Readings() {
			}
		}()

		close(start)
		time.Sleep(time.Millisecond)
		m.closeReadings()

		wg.Wait()
	}
}
