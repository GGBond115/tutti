package agenthost

import (
	"reflect"
	"testing"

	storesqlite "github.com/tutti-os/tutti/packages/agent/store-sqlite"
)

func TestEditRetryProviderHistoryBoundaryTrustsHistoricalPrefix(t *testing.T) {
	turns := []storesqlite.Turn{
		{TurnID: "turn-historical"},
		{TurnID: "turn-latest", RootProviderTurnID: "provider-latest"},
	}
	snapshot := RuntimeHistorySnapshot{
		Turns: []RuntimeHistoryTurn{
			{ID: "provider-historical"},
			{ID: "provider-latest"},
		},
	}

	got, err := editRetryProviderHistoryBoundary(turns, "turn-latest", snapshot)
	if err != nil {
		t.Fatalf("editRetryProviderHistoryBoundary() error = %v", err)
	}
	want := []string{"provider-historical", "provider-latest"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("editRetryProviderHistoryBoundary() = %#v, want %#v", got, want)
	}
}

func TestEditRetryProviderHistoryBoundaryRejectsDivergence(t *testing.T) {
	turns := []storesqlite.Turn{
		{TurnID: "turn-historical"},
		{TurnID: "turn-latest", RootProviderTurnID: "provider-latest"},
	}
	tests := []struct {
		name     string
		targetID string
		snapshot RuntimeHistorySnapshot
	}{
		{
			name:     "target is not latest",
			targetID: "turn-historical",
			snapshot: RuntimeHistorySnapshot{Turns: []RuntimeHistoryTurn{
				{ID: "provider-historical"},
				{ID: "provider-latest"},
			}},
		},
		{
			name:     "provider length differs",
			targetID: "turn-latest",
			snapshot: RuntimeHistorySnapshot{Turns: []RuntimeHistoryTurn{
				{ID: "provider-latest"},
			}},
		},
		{
			name:     "provider boundary differs",
			targetID: "turn-latest",
			snapshot: RuntimeHistorySnapshot{Turns: []RuntimeHistoryTurn{
				{ID: "provider-historical"},
				{ID: "provider-other"},
			}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := editRetryProviderHistoryBoundary(turns, test.targetID, test.snapshot); err == nil {
				t.Fatal("editRetryProviderHistoryBoundary() error = nil")
			}
		})
	}
}

func TestEditRetryHasTargetDescendantOnlyMatchesEditedTurn(t *testing.T) {
	children := []storesqlite.Session{
		{ID: "child-historical", RootTurnID: "turn-historical"},
	}
	if editRetryHasTargetDescendant(children, "turn-latest") {
		t.Fatal("historical descendant blocked editing the latest turn")
	}

	children = append(children, storesqlite.Session{
		ID: "child-latest", RootTurnID: "turn-latest",
	})
	if !editRetryHasTargetDescendant(children, "turn-latest") {
		t.Fatal("descendant of the edited turn was not detected")
	}
}
