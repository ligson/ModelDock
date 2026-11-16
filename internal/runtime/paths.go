package runtime

import (
	"os"
	"path/filepath"
)

// DefaultRoot returns the per-user location used for ModelDock runtime packages.
func DefaultRoot() string {
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "ModelDock", "runtimes")
	}
	if dir, err := os.UserCacheDir(); err == nil {
		return filepath.Join(dir, "ModelDock", "runtimes")
	}
	return ""
}

func ConfiguredRoot() string {
	if value := os.Getenv("MODELDOCK_RUNTIME_ROOT"); value != "" {
		return value
	}
	return DefaultRoot()
}
