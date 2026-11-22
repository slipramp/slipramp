package gate

import "errors"

// Evidence is the bounded public state of a repository evaluator or Server Gate.
type Evidence struct {
	Payload EvidencePayload
}

type EvidencePayload interface {
	Type() string
	Validate() error
}

func (e Evidence) MarshalJSON() ([]byte, error) {
	if e.Payload == nil {
		return nil, errors.New("missing Gate evidence")
	}
	if err := e.Payload.Validate(); err != nil {
		return nil, err
	}
	return encodeTagged(e.Payload)
}

func (e *Evidence) UnmarshalJSON(data []byte) error {
	name, raw, err := splitTaggedPayload(data)
	if err != nil {
		return err
	}
	var payload EvidencePayload
	switch name {
	case LockType:
		payload = &ScopeLock{}
	case WindowType:
		payload = &WindowEvidence{}
	default:
		codec, err := lookupEvaluator(name)
		if err != nil {
			return err
		}
		payload = codec.newEvidence()
	}
	if err := decodePayload(raw, payload); err != nil {
		return err
	}
	if err := payload.Validate(); err != nil {
		return err
	}
	e.Payload = payload
	return nil
}
