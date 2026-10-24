package cluster

import "time"

const connectionWindow = time.Minute

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

func DeriveConnectionState(observation Observation, at time.Time) ConnectionState {
	if at.Sub(observation.LastSeenAt) <= connectionWindow {
		return ConnectionStateConnected
	}

	return ConnectionStateUnreachable
}
