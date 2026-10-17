package cluster

import "time"

type ConnectionState string

const (
	ConnectionStateConnected   ConnectionState = "connected"
	ConnectionStateUnreachable ConnectionState = "unreachable"
)

type Observation struct {
	Name        string
	FirstSeenAt time.Time
	LastSeenAt  time.Time
}

type Status struct {
	Observation
	ConnectionState ConnectionState
}
