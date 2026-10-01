// Package secrets stores BYOK API keys in the OS credential store (Windows Credential Manager).
package secrets

import (
	"errors"
	"os"
	"strings"

	"github.com/zalando/go-keyring"
)

const service = "PetAI"

var envOverride = map[string]string{
	"anthropic": "PETAI_API_KEY_ANTHROPIC",
	"openai":    "PETAI_API_KEY_OPENAI",
}

func valid(provider string) bool {
	_, ok := envOverride[provider]
	return ok
}

// Get returns the API key for provider; the env override (testing) wins over the keyring.
func Get(provider string) string {
	if !valid(provider) {
		return ""
	}
	if v := strings.TrimSpace(os.Getenv(envOverride[provider])); v != "" {
		return v
	}
	v, err := keyring.Get(service, provider)
	if err != nil {
		return ""
	}
	return v
}

func Set(provider, key string) error {
	if !valid(provider) {
		return errors.New("unknown provider")
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return Delete(provider)
	}
	return keyring.Set(service, provider, key)
}

func Delete(provider string) error {
	if !valid(provider) {
		return errors.New("unknown provider")
	}
	err := keyring.Delete(service, provider)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}

// Masked returns a display-safe hint like "sk-a…9f2c" or "" when unset.
func Masked(provider string) string {
	return Mask(Get(provider))
}

func Mask(k string) string {
	if k == "" {
		return ""
	}
	if len(k) <= 8 {
		return "••••"
	}
	return k[:4] + "…" + k[len(k)-4:]
}
