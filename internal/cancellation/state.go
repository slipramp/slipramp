// Package cancellation describes operator intent independently of an execution outcome.
package cancellation

type State string

const (
	NotRequested State = "not-requested"
	Requested    State = "requested"
	Completed    State = "completed"
	Overtaken    State = "overtaken"
)

func (s State) Valid() bool {
	return s == NotRequested || s == Requested || s == Completed || s == Overtaken
}
