package cluster

import (
	"errors"
	"strings"
	"time"
)

const ConnectionWindow = time.Minute

const MaximumControllerVersionLength = 128

var ErrNotFound = errors.New("cluster has not been observed")

type ConnectionState string

const (
	ConnectionStateConnected   ConnectionState = "connected"
	ConnectionStateUnreachable ConnectionState = "unreachable"
)

type Observation struct {
	Name                string
	FirstSeenAt         time.Time
	LastSeenAt          time.Time
	ControllerBuildInfo *ControllerBuildInfo
}

type ControllerBuildInfo struct {
	Version string
}

func (i ControllerBuildInfo) Validate() error {
	if strings.TrimSpace(i.Version) == "" {
		return errors.New("controller version is required")
	}
	if len(i.Version) > MaximumControllerVersionLength {
		return errors.New("controller version is too long")
	}

	return nil
}

type Status struct {
	Observation
	ConnectionState ConnectionState
}

func DeriveConnectionState(observation Observation, at time.Time) ConnectionState {
	if at.Sub(observation.LastSeenAt) <= ConnectionWindow {
		return ConnectionStateConnected
	}

	return ConnectionStateUnreachable
}
