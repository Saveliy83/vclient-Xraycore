package config

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	subscriptionTimeout      = 30 * time.Second
	maxSubscriptionSize      = 5 << 20
	maxSubscriptionCacheSize = 10 << 20
)

var errSubscriptionTooLarge = errors.New("subscription body exceeds maximum allowed size")

// ParseSubscription разбирает список ссылок VLESS в открытом виде или в base64.
func ParseSubscription(body []byte) ([]Profile, []error) {
	content := body
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil, nil
	}

	if !bytes.HasPrefix(bytes.ToLower(trimmed), []byte("vless://")) {
		if decoded, ok := decodeSubscription(trimmed); ok {
			content = decoded
		}
	}

	lines := strings.Split(string(content), "\n")
	profiles := make([]Profile, 0, len(lines))
	var parseErrors []error
	for lineNumber, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		profile, err := ParseVLESSLink(line)
		if err != nil {
			parseErrors = append(parseErrors, fmt.Errorf("subscription line %d: %w", lineNumber+1, err))
			continue
		}
		profiles = append(profiles, profile)
	}
	return profiles, parseErrors
}

func decodeSubscription(encoded []byte) ([]byte, bool) {
	compact := bytes.Map(func(char rune) rune {
		if char == ' ' || char == '\t' || char == '\r' || char == '\n' {
			return -1
		}
		return char
	}, encoded)
	decoders := []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	}
	for _, decoder := range decoders {
		decoded, err := decoder.DecodeString(string(compact))
		if err == nil {
			return decoded, true
		}
	}
	return nil, false
}

// FetchSubscription загружает подписку с ограничением времени и размера ответа.
func FetchSubscription(ctx context.Context, subscriptionURL string, client *http.Client) ([]Profile, error) {
	if ctx == nil {
		return nil, fmt.Errorf("fetch subscription: %w", errors.New("context must not be nil"))
	}
	requestContext, cancel := context.WithTimeout(ctx, subscriptionTimeout)
	defer cancel()

	request, err := http.NewRequestWithContext(requestContext, http.MethodGet, subscriptionURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create subscription request: %w", err)
	}
	if client == nil {
		client = http.DefaultClient
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("fetch subscription: %w", err)
	}

	body, readErr := io.ReadAll(io.LimitReader(response.Body, maxSubscriptionSize+1))
	closeErr := response.Body.Close()
	if readErr != nil {
		return nil, fmt.Errorf("read subscription response: %w", readErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close subscription response: %w", closeErr)
	}
	if len(body) > maxSubscriptionSize {
		return nil, fmt.Errorf("fetch subscription: %w", errSubscriptionTooLarge)
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch subscription: unexpected HTTP status %d: %w", response.StatusCode, errors.New(http.StatusText(response.StatusCode)))
	}

	profiles, parseErrors := ParseSubscription(body)
	if len(parseErrors) != 0 {
		return profiles, fmt.Errorf("parse subscription: %w", errors.Join(parseErrors...))
	}
	return profiles, nil
}

// SelectProfile выбирает профиль по имени; при пустом имени выбирается первый.
func SelectProfile(profiles []Profile, name string) (Profile, error) {
	if len(profiles) == 0 {
		return Profile{}, fmt.Errorf("select subscription profile: %w", errors.New("subscription contains no profiles"))
	}
	if name == "" {
		return profiles[0], nil
	}
	for _, profile := range profiles {
		if profile.Name == name {
			return profile, nil
		}
	}
	return Profile{}, fmt.Errorf("select subscription profile %q: %w", name, errors.New("profile not found"))
}

// SaveSubscriptionCache атомарно сохраняет последнюю успешно загруженную подписку.
func SaveSubscriptionCache(path string, profiles []Profile) (result error) {
	if len(profiles) == 0 {
		return fmt.Errorf("save subscription cache: %w", errors.New("subscription contains no profiles"))
	}
	for index, profile := range profiles {
		if err := profile.Validate(); err != nil {
			return fmt.Errorf("save subscription cache profile %d: %w", index, err)
		}
	}
	body, err := json.MarshalIndent(profiles, "", "  ")
	if err != nil {
		return fmt.Errorf("encode subscription cache: %w", err)
	}
	if len(body) > maxSubscriptionCacheSize {
		return fmt.Errorf("save subscription cache: %w", errors.New("encoded cache exceeds maximum allowed size"))
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create subscription cache directory: %w", err)
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".subscription-*.tmp")
	if err != nil {
		return fmt.Errorf("create subscription cache file: %w", err)
	}
	tempPath := file.Name()
	defer func() {
		if err := os.Remove(tempPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			result = errors.Join(result, fmt.Errorf("remove temporary subscription cache: %w", err))
		}
	}()
	if _, err := file.Write(body); err != nil {
		_ = file.Close()
		return fmt.Errorf("write subscription cache: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync subscription cache: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close subscription cache: %w", err)
	}
	if err := restrictCacheFile(tempPath); err != nil {
		return fmt.Errorf("secure subscription cache: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace subscription cache: %w", err)
	}
	return nil
}

// LoadSubscriptionCache загружает и валидирует сохранённую подписку.
func LoadSubscriptionCache(path string) ([]Profile, error) {
	file, err := os.Open(path) // #nosec G304 -- path is the locally configured subscription cache.
	if err != nil {
		return nil, fmt.Errorf("read subscription cache %q: %w", path, err)
	}
	body, err := io.ReadAll(io.LimitReader(file, maxSubscriptionCacheSize+1))
	closeErr := file.Close()
	if err != nil {
		return nil, fmt.Errorf("read subscription cache %q: %w", path, err)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close subscription cache %q: %w", path, closeErr)
	}
	if len(body) > maxSubscriptionCacheSize {
		return nil, fmt.Errorf("read subscription cache %q: %w", path, errors.New("cache exceeds maximum allowed size"))
	}
	var profiles []Profile
	if err := json.Unmarshal(body, &profiles); err != nil {
		return nil, fmt.Errorf("decode subscription cache %q: %w", path, err)
	}
	if len(profiles) == 0 {
		return nil, fmt.Errorf("validate subscription cache %q: %w", path, errors.New("cache contains no profiles"))
	}
	for index, profile := range profiles {
		if err := profile.Validate(); err != nil {
			return nil, fmt.Errorf("validate subscription cache profile %d: %w", index, err)
		}
	}
	return profiles, nil
}
