package controllercommand

import (
	"testing"
	"time"
)

func TestWakeupsNotifyCurrentClusterWatchers(t *testing.T) {
	t.Parallel()

	wakeups := NewWakeups()
	first := wakeups.Watch("colo")
	second := wakeups.Watch("colo")
	other := wakeups.Watch("edge")

	wakeups.Notify("colo")

	for name, wakeup := range map[string]<-chan struct{}{"first": first, "second": second} {
		select {
		case <-wakeup:
		case <-time.After(time.Second):
			t.Fatalf("%s watcher was not notified", name)
		}
	}
	select {
	case <-other:
		t.Fatal("another Cluster's watcher was notified")
	default:
	}

	next := wakeups.Watch("colo")
	select {
	case <-next:
		t.Fatal("replacement watcher was already notified")
	default:
	}
}

func TestWakeupsDoNotRetainNotificationsWithoutWatchers(t *testing.T) {
	t.Parallel()

	wakeups := NewWakeups()
	wakeups.Notify("colo")

	select {
	case <-wakeups.Watch("colo"):
		t.Fatal("notification was retained before a watcher registered")
	default:
	}
}
