package agentsessionreplay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	replay "github.com/tutti-os/tutti/packages/agent/session-replay"
	replaybiz "github.com/tutti-os/tutti/services/tuttid/biz/agentsessionreplay"
)

type SemanticCassetteReader struct {
	directories map[string]string
}

func NewSemanticCassetteReader(
	directories map[string]string,
) (*SemanticCassetteReader, error) {
	resolved := make(map[string]string, len(directories))
	for cassetteID, directory := range directories {
		cassetteID = strings.TrimSpace(cassetteID)
		directory = strings.TrimSpace(directory)
		if cassetteID == "" || directory == "" {
			return nil, errors.New("semantic cassette reader requires cassette ID and directory")
		}
		resolved[cassetteID] = directory
	}
	return &SemanticCassetteReader{directories: resolved}, nil
}

func (r *SemanticCassetteReader) ReadSemanticCassette(
	ctx context.Context,
	cassetteID string,
) (replaybiz.SemanticCassetteArtifact, error) {
	if err := ctx.Err(); err != nil {
		return replaybiz.SemanticCassetteArtifact{}, err
	}
	cassetteID = strings.TrimSpace(cassetteID)
	if r == nil || cassetteID == "" {
		return replaybiz.SemanticCassetteArtifact{}, errors.New(
			"semantic cassette reader requires cassette ID",
		)
	}
	directory := strings.TrimSpace(r.directories[cassetteID])
	if directory == "" {
		return replaybiz.SemanticCassetteArtifact{}, fmt.Errorf(
			"semantic cassette %q is not registered",
			cassetteID,
		)
	}

	manifestRaw, err := os.ReadFile(
		filepath.Join(directory, replay.CassetteManifestFile),
	)
	if err != nil {
		return replaybiz.SemanticCassetteArtifact{}, err
	}
	var manifest replay.CassetteManifest
	if err := json.Unmarshal(manifestRaw, &manifest); err != nil {
		return replaybiz.SemanticCassetteArtifact{}, err
	}
	if err := rejectPortableScopeFields(manifestRaw, "cassette manifest"); err != nil {
		return replaybiz.SemanticCassetteArtifact{}, err
	}
	if manifest.ID != cassetteID {
		return replaybiz.SemanticCassetteArtifact{}, errors.New(
			"cassette identity mismatch",
		)
	}
	if manifest.StateFormat != replaybiz.StateFormat {
		return replaybiz.SemanticCassetteArtifact{}, errors.New(
			"unsupported cassette state format",
		)
	}
	blobManifest, err := readBlobManifest(
		filepath.Join(directory, replay.BlobManifestFile),
	)
	if err != nil {
		return replaybiz.SemanticCassetteArtifact{}, err
	}
	if err := replay.ValidateCassetteManifestPolicy(manifest, blobManifest); err != nil {
		return replaybiz.SemanticCassetteArtifact{}, err
	}
	files, err := collectCassetteFiles(directory, manifest.Files)
	if err != nil {
		return replaybiz.SemanticCassetteArtifact{}, err
	}
	if err := replay.ValidateCassetteIntegrity(manifest, files); err != nil {
		return replaybiz.SemanticCassetteArtifact{}, err
	}
	if err := validatePortableReplayFiles(directory); err != nil {
		return replaybiz.SemanticCassetteArtifact{}, err
	}

	events, err := readJSONLines[replay.ActivityEvent](
		filepath.Join(directory, replay.ActivityEventsFile),
		"activity event",
	)
	if err != nil {
		return replaybiz.SemanticCassetteArtifact{}, err
	}
	plan, err := loadAndValidateCheckpointPlan(directory, events)
	if err != nil {
		return replaybiz.SemanticCassetteArtifact{}, err
	}
	expected, _, err := readSemanticReplayState(
		filepath.Join(directory, replay.ExpectedStateFile),
	)
	if err != nil {
		return replaybiz.SemanticCassetteArtifact{}, err
	}

	artifact := replaybiz.SemanticCassetteArtifact{
		Manifest:       manifest,
		ExpectedState:  expected,
		CheckpointPlan: plan,
	}
	if manifest.Mode == replay.ScenarioModeContinueSession {
		initial, raw, err := readSemanticReplayState(
			filepath.Join(directory, replay.InitialStateFile),
		)
		if err != nil {
			return replaybiz.SemanticCassetteArtifact{}, err
		}
		artifact.InitialStateRaw = raw
		artifact.InitialState = &initial
	}
	return artifact, nil
}

func readSemanticReplayState(
	path string,
) (replaybiz.TuttiReplayState, []byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return replaybiz.TuttiReplayState{}, nil, err
	}
	var state replaybiz.TuttiReplayState
	if err := json.Unmarshal(raw, &state); err != nil {
		return replaybiz.TuttiReplayState{}, nil, fmt.Errorf(
			"decode semantic replay state %s: %w",
			filepath.Base(path),
			err,
		)
	}
	if err := replaybiz.ValidateTuttiReplayState(state); err != nil {
		return replaybiz.TuttiReplayState{}, nil, err
	}
	return state, raw, nil
}
