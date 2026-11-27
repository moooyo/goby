package config

import (
	"os"
	"slices"
	"strings"
	"testing"
)

func TestLoadNFORecoveryRootsUsesMediaListConvention(t *testing.T) {
	startupConfigEnvironment(t)
	const variable = "GOBY_NFO_RECOVERY_ROOTS"
	t.Setenv(variable, "")
	cfg, err := Load()
	if err != nil || len(cfg.NFORecoveryRoots) != 0 {
		t.Fatalf("default root declaration = %v, error = %v", cfg.NFORecoveryRoots, err)
	}
	t.Setenv("GOBY_MEDIA_ROOTS", "/media")
	paths := []string{"/media/Movies", "/media/TV Shows"}
	t.Setenv(variable, strings.Join(paths, string(os.PathListSeparator)))
	cfg, err = Load()
	if err != nil || !slices.Equal(cfg.NFORecoveryRoots, paths) || !slices.Equal(cfg.MediaRoots, []string{"/media"}) {
		t.Fatalf("root declaration = %v, ordinary allowlist = %v, error = %v", cfg.NFORecoveryRoots, cfg.MediaRoots, err)
	}
}

func TestLoadNFORecoveryRootsRejectsMalformedDeclarations(t *testing.T) {
	startupConfigEnvironment(t)
	t.Setenv("GOBY_MEDIA_ROOTS", "/media")
	const variable = "GOBY_NFO_RECOVERY_ROOTS"
	for _, value := range []string{"media/movies", "/media/../movies", "/media//movies", "/media/movies/", "/media/movies" + string(os.PathListSeparator)} {
		t.Setenv(variable, value)
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), variable) {
			t.Errorf("declaration %q = %v, want a named configuration error", value, err)
		}
	}
	t.Setenv(variable, "/media/movies")
	t.Setenv("GOBY_MEDIA_ROOTS", "")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), variable) {
		t.Errorf("declaration without a media allowlist = %v, want a named configuration error", err)
	}
}
