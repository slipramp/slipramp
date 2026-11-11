package controllercommand

import "sync"

// Wakeups lets command producers interrupt a Controller's long poll after
// durable state changes. The command scheduler remains authoritative; a wakeup
// is only a hint to check it again.
type Wakeups struct {
	mutex    sync.Mutex
	clusters map[string]chan struct{}
}

func NewWakeups() *Wakeups {
	return &Wakeups{clusters: make(map[string]chan struct{})}
}

// Watch returns a channel that closes when work for the Cluster may have
// changed. Callers must select a new channel after every wakeup.
func (w *Wakeups) Watch(clusterName string) <-chan struct{} {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	wakeup := w.clusters[clusterName]
	if wakeup == nil {
		wakeup = make(chan struct{})
		w.clusters[clusterName] = wakeup
	}

	return wakeup
}

// Notify wakes every current watcher for the Cluster. Notifications are not
// retained because each watcher registers before consulting durable state.
func (w *Wakeups) Notify(clusterName string) {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	wakeup := w.clusters[clusterName]
	if wakeup == nil {
		return
	}

	close(wakeup)
	delete(w.clusters, clusterName)
}
