package appservice

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const assetManifestFilename = ".bundle-assets.json"

var runtimeAssetRoots = []string{"conf", ".agents", "scripts", filepath.Join("web", "dist")}

type assetManifest struct {
	Version int               `json:"version"`
	Files   map[string]string `json:"files"`
}

func Prepare(layout Layout) error {
	if err := validateBundle(layout); err != nil {
		return err
	}
	for _, directory := range []string{
		layout.StateRoot,
		layout.RuntimeRoot,
		layout.LogDirectory,
		layout.QdrantWorking,
		filepath.Join(layout.QdrantWorking, "storage"),
		filepath.Join(layout.QdrantWorking, "snapshots"),
		filepath.Join(layout.RuntimeRoot, "var"),
		filepath.Join(layout.RuntimeRoot, "runs"),
		filepath.Join(layout.RuntimeRoot, "data"),
		filepath.Dir(layout.CCConnectConfig),
	} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return fmt.Errorf("create app runtime directory %q: %w", directory, err)
		}
	}
	if err := syncRuntimeAssets(layout); err != nil {
		return err
	}
	if err := ensureFile(filepath.Join(layout.RuntimeRoot, "data", "shared-memory.md"), 0o600); err != nil {
		return fmt.Errorf("create shared memory file: %w", err)
	}
	return ensureBootstrapOverride(filepath.Join(layout.RuntimeRoot, "conf", "config.runtime.yaml"))
}

func validateBundle(layout Layout) error {
	for _, path := range []string{
		layout.ServerBinary,
		layout.QdrantBinary,
		layout.CCConnectBinary,
		filepath.Join(layout.ResourceRoot, "conf", "config.yaml"),
		filepath.Join(layout.ResourceRoot, "conf", "qdrant.yaml"),
		filepath.Join(layout.ResourceRoot, "web", "dist", "index.html"),
		filepath.Join(layout.ResourceRoot, ".agents", "skills"),
		filepath.Join(layout.ResourceRoot, "scripts", "jarvis-tools"),
	} {
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("required bundled resource %q: %w", path, err)
		}
		if path == layout.ServerBinary || path == layout.QdrantBinary {
			if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
				return fmt.Errorf("bundled executable is not executable: %s", path)
			}
		}
	}
	return nil
}

func syncRuntimeAssets(layout Layout) error {
	manifestPath := filepath.Join(layout.StateRoot, assetManifestFilename)
	previous, err := readAssetManifest(manifestPath)
	if err != nil {
		return err
	}
	next := assetManifest{Version: 1, Files: make(map[string]string)}
	for _, root := range runtimeAssetRoots {
		sourceRoot := filepath.Join(layout.ResourceRoot, root)
		err := filepath.WalkDir(sourceRoot, func(sourcePath string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			relative, err := filepath.Rel(layout.ResourceRoot, sourcePath)
			if err != nil {
				return err
			}
			relative = filepath.Clean(relative)
			if shouldSkipRuntimeAsset(relative, entry) {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			targetPath := filepath.Join(layout.RuntimeRoot, relative)
			if entry.IsDir() {
				return os.MkdirAll(targetPath, 0o755)
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("bundled runtime asset must not be a symlink: %s", sourcePath)
			}
			if !entry.Type().IsRegular() {
				return nil
			}
			sourceHash, err := fileHash(sourcePath)
			if err != nil {
				return err
			}
			next.Files[relative] = sourceHash
			targetHash, targetErr := fileHash(targetPath)
			if targetErr == nil {
				oldHash := previous.Files[relative]
				if targetHash != oldHash && targetHash != sourceHash {
					return nil
				}
				if targetHash == sourceHash {
					return nil
				}
			} else if !errors.Is(targetErr, os.ErrNotExist) {
				return targetErr
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			return copyFileAtomic(sourcePath, targetPath, info.Mode().Perm())
		})
		if err != nil {
			return fmt.Errorf("sync bundled runtime root %q: %w", root, err)
		}
	}
	if err := removeObsoleteUnmodifiedAssets(layout.RuntimeRoot, previous, next); err != nil {
		return err
	}
	return writeJSONAtomic(manifestPath, next, 0o600)
}

func shouldSkipRuntimeAsset(relative string, entry fs.DirEntry) bool {
	name := entry.Name()
	if name == ".DS_Store" || name == "node_modules" {
		return true
	}
	return filepath.ToSlash(relative) == "conf/config.runtime.yaml"
}

func removeObsoleteUnmodifiedAssets(runtimeRoot string, previous, next assetManifest) error {
	paths := make([]string, 0)
	for relative, oldHash := range previous.Files {
		if _, exists := next.Files[relative]; exists {
			continue
		}
		target := filepath.Join(runtimeRoot, filepath.Clean(relative))
		currentHash, err := fileHash(target)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if currentHash == oldHash {
			paths = append(paths, target)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(paths)))
	for _, path := range paths {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove obsolete runtime asset %q: %w", path, err)
		}
	}
	return nil
}

func ensureBootstrapOverride(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect runtime config %q: %w", path, err)
	}
	const content = `# Desktop bootstrap defaults. The onboarding flow enables selected capabilities.
extract:
  enabled: false
  principal_open_id: "ou_desktop_onboarding_pending"
execute:
  enabled: false
factengine:
  enabled: false
proactive:
  enabled: false
meeting_sweep:
  enabled: false
morning_brief:
  enabled: false
scheduled_task:
  enabled: false
dailydigest:
  enabled: false
  git_author: "__desktop_onboarding_pending__"
`
	if err := writeFileAtomic(path, []byte(content), 0o600); err != nil {
		return fmt.Errorf("create desktop bootstrap config: %w", err)
	}
	return nil
}

func readAssetManifest(path string) (assetManifest, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return assetManifest{Version: 1, Files: map[string]string{}}, nil
	}
	if err != nil {
		return assetManifest{}, fmt.Errorf("read asset manifest: %w", err)
	}
	var manifest assetManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return assetManifest{}, fmt.Errorf("decode asset manifest: %w", err)
	}
	if manifest.Files == nil {
		manifest.Files = map[string]string{}
	}
	return manifest, nil
}

func fileHash(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("hash %q: %w", path, err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func copyFileAtomic(source, target string, mode fs.FileMode) error {
	raw, err := os.ReadFile(source)
	if err != nil {
		return fmt.Errorf("read bundled asset %q: %w", source, err)
	}
	if err := writeFileAtomic(target, raw, mode); err != nil {
		return fmt.Errorf("write runtime asset %q: %w", target, err)
	}
	return nil
}

func writeJSONAtomic(path string, value any, mode fs.FileMode) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, append(raw, '\n'), mode)
}

func writeFileAtomic(path string, raw []byte, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if _, err := temp.Write(raw); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Chmod(mode); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}

func ensureFile(path string, mode fs.FileMode) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return file.Close()
}

func prependPath(environment []string, directories ...string) []string {
	filtered := make([]string, 0, len(environment)+1)
	current := ""
	for _, item := range environment {
		if strings.HasPrefix(item, "PATH=") {
			current = strings.TrimPrefix(item, "PATH=")
			continue
		}
		filtered = append(filtered, item)
	}
	parts := make([]string, 0, len(directories)+1)
	for _, directory := range directories {
		if strings.TrimSpace(directory) != "" {
			parts = append(parts, directory)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}
	return append(filtered, "PATH="+strings.Join(parts, string(os.PathListSeparator)))
}
