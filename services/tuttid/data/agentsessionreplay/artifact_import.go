package agentsessionreplay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	replay "github.com/tutti-os/tutti/packages/agent/session-replay"
)

var ErrCassetteAlreadyExists = errors.New("agent session cassette already exists")

const maxCassetteManifestBytes = 1 << 20

func (s *Store) Import(
	ctx context.Context,
	sourceDirectory string,
) (replay.Artifact, error) {
	sourceDirectory = filepath.Clean(strings.TrimSpace(sourceDirectory))
	if sourceDirectory == "." || !filepath.IsAbs(sourceDirectory) {
		return replay.Artifact{}, errors.New("cassette import directory must be absolute")
	}
	info, err := os.Stat(sourceDirectory)
	if err != nil {
		return replay.Artifact{}, err
	}
	if !info.IsDir() {
		return replay.Artifact{}, errors.New("cassette import source is not a directory")
	}
	manifest, err := readImportCassetteManifest(sourceDirectory)
	if err != nil {
		return replay.Artifact{}, err
	}
	destination := s.cassetteLayout(manifest.ID)
	if _, err := os.Lstat(destination.StorageKey); err == nil {
		return replay.Artifact{}, errors.Join(replay.ErrInvalidState, ErrCassetteAlreadyExists)
	} else if !errors.Is(err, os.ErrNotExist) {
		return replay.Artifact{}, err
	}
	parent := filepath.Dir(destination.StorageKey)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return replay.Artifact{}, err
	}
	staging, err := os.MkdirTemp(parent, ".import-*")
	if err != nil {
		return replay.Artifact{}, err
	}
	defer func() { _ = os.RemoveAll(staging) }()
	if err := copyCassetteDirectory(sourceDirectory, staging); err != nil {
		return replay.Artifact{}, err
	}
	if err := os.Rename(staging, destination.StorageKey); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return replay.Artifact{}, errors.Join(replay.ErrInvalidState, ErrCassetteAlreadyExists)
		}
		return replay.Artifact{}, fmt.Errorf("commit cassette import: %w", err)
	}
	artifact, err := s.Resolve(ctx, replay.Cassette{ID: manifest.ID})
	if err != nil {
		_ = os.RemoveAll(destination.StorageKey)
		return replay.Artifact{}, fmt.Errorf("validate imported cassette: %w", err)
	}
	return artifact, nil
}

func readImportCassetteManifest(
	sourceDirectory string,
) (replay.CassetteManifest, error) {
	path := filepath.Join(sourceDirectory, replay.CassetteManifestFile)
	file, err := os.Open(path)
	if err != nil {
		return replay.CassetteManifest{}, err
	}
	defer file.Close()
	var manifest replay.CassetteManifest
	decoder := json.NewDecoder(io.LimitReader(file, maxCassetteManifestBytes))
	if err := decoder.Decode(&manifest); err != nil {
		return replay.CassetteManifest{}, fmt.Errorf("decode cassette manifest: %w", err)
	}
	if strings.TrimSpace(manifest.ID) == "" {
		return replay.CassetteManifest{}, errors.New("cassette manifest identity is required")
	}
	return manifest, nil
}

func copyCassetteDirectory(sourceDirectory, destinationDirectory string) error {
	var copiedBytes int64
	return filepath.WalkDir(sourceDirectory, func(
		sourcePath string,
		entry fs.DirEntry,
		walkErr error,
	) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(sourceDirectory, sourcePath)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("cassette import contains symbolic link %q", relative)
		}
		destinationPath := filepath.Join(destinationDirectory, relative)
		if entry.IsDir() {
			return os.Mkdir(destinationPath, 0o700)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("cassette import file %q is not regular", relative)
		}
		copiedBytes += info.Size()
		if copiedBytes > replay.MaxCassetteBytes+maxCassetteManifestBytes {
			return errors.New("cassette import exceeds the size limit")
		}
		source, err := os.Open(sourcePath)
		if err != nil {
			return err
		}
		destination, createErr := os.OpenFile(
			destinationPath,
			os.O_CREATE|os.O_EXCL|os.O_WRONLY,
			0o600,
		)
		if createErr != nil {
			_ = source.Close()
			return createErr
		}
		_, copyErr := io.Copy(destination, source)
		return errors.Join(copyErr, destination.Close(), source.Close())
	})
}
