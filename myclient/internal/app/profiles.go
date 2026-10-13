package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"

	"example.com/vclient/internal/config"
)

const profileFileName = "profiles.json"

type ProfileStore struct {
	mu       sync.RWMutex
	path     string
	profiles []config.Profile
	readOnly bool
}

func NewProfileStore(path string) (*ProfileStore, error) {
	if path == "" {
		return nil, fmt.Errorf("create profile store: %w", errors.New("path must not be empty"))
	}
	store := &ProfileStore{path: path}
	data, err := os.ReadFile(path) // #nosec G304 -- path is the explicitly selected profile store.
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read profile store %q: %w", path, err)
	}
	var profiles []config.Profile
	if err := json.Unmarshal(data, &profiles); err != nil {
		backupPath := path + ".bak"
		if _, statErr := os.Stat(backupPath); statErr == nil {
			store.readOnly = true
			return store, fmt.Errorf("backup corrupt profile store %q: %w", backupPath, errors.New("backup already exists; original file was preserved"))
		} else if !errors.Is(statErr, os.ErrNotExist) {
			store.readOnly = true
			return store, fmt.Errorf("check profile store backup %q: %w", backupPath, statErr)
		}
		if renameErr := os.Rename(path, backupPath); renameErr != nil {
			store.readOnly = true
			return store, fmt.Errorf("preserve corrupt profile store %q: %w", path, renameErr)
		}
		return store, fmt.Errorf("decode profile store %q: %w", path, err)
	}
	for index, profile := range profiles {
		if err := profile.Validate(); err != nil {
			backupPath := path + ".bak"
			if _, statErr := os.Stat(backupPath); statErr == nil {
				store.readOnly = true
				return store, fmt.Errorf("backup invalid profile store %q: %w", backupPath, errors.New("backup already exists; original file was preserved"))
			} else if !errors.Is(statErr, os.ErrNotExist) {
				store.readOnly = true
				return store, fmt.Errorf("check profile store backup %q: %w", backupPath, statErr)
			}
			if renameErr := os.Rename(path, backupPath); renameErr != nil {
				store.readOnly = true
				return store, fmt.Errorf("preserve invalid profile store %q: %w", path, renameErr)
			}
			return store, fmt.Errorf("validate profile store entry %d: %w", index, err)
		}
	}
	store.profiles = cloneProfiles(profiles)
	return store, nil
}

func OpenProfileStore() (*ProfileStore, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("resolve user config directory: %w", err)
	}
	return NewProfileStore(filepath.Join(configDir, "vclient", profileFileName))
}

func (store *ProfileStore) List() []config.Profile {
	store.mu.RLock()
	defer store.mu.RUnlock()
	return cloneProfiles(store.profiles)
}

func (store *ProfileStore) Add(profile config.Profile) error {
	_, _, err := store.add(profile)
	return err
}

func (store *ProfileStore) add(profile config.Profile) (config.Profile, bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := profile.Validate(); err != nil {
		return config.Profile{}, false, fmt.Errorf("add profile: %w", err)
	}
	for _, existing := range store.profiles {
		if sameProfileConfiguration(existing, profile) {
			return cloneProfile(existing), false, nil
		}
	}
	profile.Name = uniqueProfileName(profile.Name, store.profiles, -1)
	profiles := append(cloneProfiles(store.profiles), cloneProfile(profile))
	if err := store.saveLocked(profiles); err != nil {
		return config.Profile{}, false, fmt.Errorf("add profile: %w", err)
	}
	store.profiles = profiles
	return cloneProfile(profile), true, nil
}

func sameProfileConfiguration(left, right config.Profile) bool {
	left.Name = ""
	right.Name = ""
	left.Warnings = nil
	right.Warnings = nil
	return reflect.DeepEqual(left, right)
}

func (store *ProfileStore) Update(index int, profile config.Profile) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if index < 0 || index >= len(store.profiles) {
		return fmt.Errorf("update profile: %w", errors.New("profile index out of range"))
	}
	if err := profile.Validate(); err != nil {
		return fmt.Errorf("update profile: %w", err)
	}
	if uniqueProfileName(profile.Name, store.profiles, index) != profile.Name {
		return fmt.Errorf("update profile: %w", errors.New("profile name already exists"))
	}
	profiles := cloneProfiles(store.profiles)
	profiles[index] = cloneProfile(profile)
	if err := store.saveLocked(profiles); err != nil {
		return fmt.Errorf("update profile: %w", err)
	}
	store.profiles = profiles
	return nil
}

func (store *ProfileStore) Delete(index int) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if index < 0 || index >= len(store.profiles) {
		return fmt.Errorf("delete profile: %w", errors.New("profile index out of range"))
	}
	profiles := cloneProfiles(store.profiles)
	profiles = append(profiles[:index], profiles[index+1:]...)
	if err := store.saveLocked(profiles); err != nil {
		return fmt.Errorf("delete profile: %w", err)
	}
	store.profiles = profiles
	return nil
}

func (store *ProfileStore) Get(index int) (config.Profile, bool) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	if index < 0 || index >= len(store.profiles) {
		return config.Profile{}, false
	}
	return cloneProfile(store.profiles[index]), true
}

func (store *ProfileStore) AddFromLink(raw string) (config.Profile, []string, error) {
	profile, err := config.ParseVLESSLink(raw)
	if err != nil {
		return config.Profile{}, nil, fmt.Errorf("parse profile link: %w", err)
	}
	warnings := append([]string(nil), profile.Warnings...)
	addedProfile, _, err := store.add(profile)
	if err != nil {
		return config.Profile{}, warnings, err
	}
	return addedProfile, warnings, nil
}

func (store *ProfileStore) AddFromSubscription(ctx context.Context, subscriptionURL string) (int, error) {
	profiles, fetchErr := config.FetchSubscription(ctx, subscriptionURL, nil)
	added := 0
	var addErrors []error
	for _, profile := range profiles {
		_, inserted, err := store.add(profile)
		if err != nil {
			addErrors = append(addErrors, err)
			continue
		}
		if inserted {
			added++
		}
	}
	if fetchErr != nil || len(addErrors) != 0 {
		return added, fmt.Errorf("import subscription: %w", errors.Join(append([]error{fetchErr}, addErrors...)...))
	}
	return added, nil
}

func (store *ProfileStore) Save() error {
	store.mu.RLock()
	defer store.mu.RUnlock()
	if err := store.saveLocked(store.profiles); err != nil {
		return fmt.Errorf("save profile store: %w", err)
	}
	return nil
}

func (store *ProfileStore) saveLocked(profiles []config.Profile) (result error) {
	if store.readOnly {
		return fmt.Errorf("write profile store: %w", errors.New("store is read-only because a corrupt file could not be preserved as backup"))
	}
	body, err := json.MarshalIndent(profiles, "", "  ")
	if err != nil {
		return fmt.Errorf("encode profiles: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(store.path), 0o700); err != nil {
		return fmt.Errorf("create profile directory: %w", err)
	}
	file, err := os.CreateTemp(filepath.Dir(store.path), ".profiles-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary profile file: %w", err)
	}
	tempPath := file.Name()
	closed := false
	defer func() {
		if !closed {
			result = errors.Join(result, file.Close())
		}
		if removeErr := os.Remove(tempPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			result = errors.Join(result, fmt.Errorf("remove temporary profile file: %w", removeErr))
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		return fmt.Errorf("set profile file permissions: %w", err)
	}
	if _, err := file.Write(body); err != nil {
		return fmt.Errorf("write temporary profile file: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync temporary profile file: %w", err)
	}
	if err := file.Close(); err != nil {
		closed = true
		return fmt.Errorf("close temporary profile file: %w", err)
	}
	closed = true
	if err := restrictAppFile(tempPath); err != nil {
		return fmt.Errorf("secure temporary profile file: %w", err)
	}
	if err := os.Rename(tempPath, store.path); err != nil {
		return fmt.Errorf("replace profile file: %w", err)
	}
	return nil
}

func uniqueProfileName(name string, profiles []config.Profile, ignoreIndex int) string {
	used := make(map[string]struct{}, len(profiles))
	for index, profile := range profiles {
		if index != ignoreIndex {
			used[profile.Name] = struct{}{}
		}
	}
	if _, exists := used[name]; !exists {
		return name
	}
	for suffix := 2; ; suffix++ {
		candidate := fmt.Sprintf("%s (%d)", name, suffix)
		if _, exists := used[candidate]; !exists {
			return candidate
		}
	}
}

func cloneProfiles(profiles []config.Profile) []config.Profile {
	clones := make([]config.Profile, len(profiles))
	for index, profile := range profiles {
		clones[index] = cloneProfile(profile)
	}
	return clones
}

func cloneProfile(profile config.Profile) config.Profile {
	profile.TLS.ALPN = append([]string(nil), profile.TLS.ALPN...)
	if profile.XHTTP.Headers != nil {
		headers := make(map[string]string, len(profile.XHTTP.Headers))
		for key, value := range profile.XHTTP.Headers {
			headers[key] = value
		}
		profile.XHTTP.Headers = headers
	}
	profile.Warnings = append([]string(nil), profile.Warnings...)
	return profile
}
