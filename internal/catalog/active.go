package catalog

// ActiveCatalog pairs the active revision with current Target state from the same database snapshot.
type ActiveCatalog struct {
	Revision     Revision
	TargetStates map[string]TargetState
}

// TargetState is the mutable projection associated with a target definition.
type TargetState struct {
	DesiredCommitSHA   string
	LastAttemptedSync  *SyncRunCheckpoint
	LastSuccessfulSync *SyncRunCheckpoint
}
