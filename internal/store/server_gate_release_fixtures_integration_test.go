package store

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/gate"
	"github.com/slipramp/slipramp/internal/productidentity"
)

func openGateTestWindow() gate.WeeklyWindow {
	return allDayGateTestWindow("sunday", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday")
}

func closedGateTestWindow(at time.Time) gate.WeeklyWindow {
	return allDayGateTestWindow(strings.ToLower(at.UTC().AddDate(0, 0, 2).Weekday().String()))
}

func allDayGateTestWindow(days ...string) gate.WeeklyWindow {
	return gate.WeeklyWindow{TimeZone: "UTC", Weekly: []gate.WeeklyInterval{{Days: days, Start: "00:00", End: "24:00"}}}
}

func setGateReleasePolicies(t *testing.T, fixture promotionIntegrationFixture, window gate.WeeklyWindow, owners ...gate.Scope) {
	t.Helper()
	for _, owner := range owners {
		if _, err := fixture.database.ChangeServerGateLock(fixture.ctx, owner, fixture.actor, &gate.LockRequest{Reason: "maintenance"}); err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.database.ChangeServerGateWindow(fixture.ctx, owner, fixture.actor, &window); err != nil {
			t.Fatal(err)
		}
	}
}

func overrideReleaseGates(t *testing.T, fixture promotionIntegrationFixture, subject gate.SubjectType, id, process uuid.UUID, want int, before func()) {
	t.Helper()
	list, err := fixture.database.ListGateOverrides(fixture.ctx, subject, id, process)
	if err != nil || len(list.Eligible) != want {
		t.Fatalf("%s %s has %d eligible Gates, want %d: %v", subject, id, len(list.Eligible), want, err)
	}
	for _, check := range list.Eligible {
		before()
		_, err := fixture.database.RecordGateOverride(fixture.ctx, gate.OverrideRequest{
			Subject: subject, SubjectID: id, Reference: check.Reference, Actor: fixture.actor,
			Reason: "restore service", EvaluatorProcessID: process, RecordedAt: time.Now().UTC(),
		})
		if err != nil {
			t.Fatalf("override %s: %v", check.Key, err)
		}
	}
}

func requireGateReleaseEvidence(
	t *testing.T,
	data []byte,
	at time.Time,
	actor productidentity.ActorSnapshot,
	owners []gate.Scope,
) {
	t.Helper()
	var release serverGateRelease
	if err := json.Unmarshal(data, &release); err != nil || !release.CheckedAt.Equal(at) || len(release.Scopes) != len(owners) {
		t.Fatalf("release evidence at %v = %s, %v", at, data, err)
	}
	for i, scope := range release.Scopes {
		if scope.Kind != owners[i].Kind || scope.LockVersion != 1 || scope.WindowVersion != 1 || scope.Lock == nil ||
			scope.Lock.Reason != "maintenance" || scope.Window == nil || scope.Window.State != gate.RequirementBlocking ||
			scope.LockOverride == nil || scope.LockOverride.Actor != actor ||
			scope.WindowOverride == nil || scope.WindowOverride.Actor != actor || scope.WindowOverride.ObservedEvidence == nil {
			t.Fatalf("frozen scope = %#v", scope)
		}
		observed, ok := scope.WindowOverride.ObservedEvidence.Payload.(*gate.WindowEvidence)
		if !ok || observed.Digest != scope.Window.Digest {
			t.Fatalf("waived window evidence = %#v, want digest %s", observed, scope.Window.Digest)
		}
	}
}
