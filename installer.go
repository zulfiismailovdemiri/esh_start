package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// defaultRegistry is the GitHub repo base (no branch/tag suffix).
// The version in esh.json require is a git ref (tag or branch, e.g. "main", "v1.0.0").
// URL pattern: {registry}/{version}/{package}/package.json
const defaultRegistry = "https://raw.githubusercontent.com/zulfiismailovdemiri/esh_repo"

// EshConfig is the structure of esh.json (like composer.json).
type EshConfig struct {
	Name        string            `json:"name"`
	Version     string            `json:"version"`
	Description string            `json:"description,omitempty"`
	Require     map[string]string `json:"require"`
	Registry    string            `json:"registry,omitempty"`
}

// EshLock is written to esh.lock and records exact resolved versions + checksums.
type EshLock struct {
	Packages map[string]LockedPackage `json:"packages"`
}

// LockedPackage records the version installed and a sha256 per downloaded file.
type LockedPackage struct {
	Version string            `json:"version"`
	Files   map[string]string `json:"files"`
}

// PackageManifest is the package.json stored in the esh_repo for each version.
type PackageManifest struct {
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Description string   `json:"description,omitempty"`
	Files       []string `json:"files"`
}

// RegistryIndex is the optional index.json at the root of the registry,
// used to resolve the "latest" version alias.
type RegistryIndex struct {
	Packages map[string]RegistryEntry `json:"packages"`
}

type RegistryEntry struct {
	Versions []string `json:"versions"`
	Latest   string   `json:"latest"`
}

// ---- esh.json helpers ----

func readEshConfig() (*EshConfig, error) {
	data, err := os.ReadFile("esh.json")
	if err != nil {
		return nil, fmt.Errorf("esh.json not found — run: esh init")
	}
	var cfg EshConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("invalid esh.json: %w", err)
	}
	if cfg.Require == nil {
		cfg.Require = make(map[string]string)
	}
	return &cfg, nil
}

func writeEshConfig(cfg *EshConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile("esh.json", append(data, '\n'), 0644)
}

// ---- esh.lock helpers ----

func loadLock() *EshLock {
	data, err := os.ReadFile("esh.lock")
	if err != nil {
		return &EshLock{Packages: make(map[string]LockedPackage)}
	}
	var l EshLock
	if err := json.Unmarshal(data, &l); err != nil {
		return &EshLock{Packages: make(map[string]LockedPackage)}
	}
	if l.Packages == nil {
		l.Packages = make(map[string]LockedPackage)
	}
	return &l
}

func saveLock(l *EshLock) error {
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile("esh.lock", append(data, '\n'), 0644)
}

// ---- registry helpers ----

func registryBase(cfg *EshConfig) string {
	if cfg.Registry != "" {
		return strings.TrimRight(cfg.Registry, "/")
	}
	return defaultRegistry
}

// resolveVersion turns "latest" into a real git ref by reading index.json
// from the main branch. Any other value is used as-is (branch or tag name).
func resolveVersion(registry, name, version string) (string, error) {
	if version != "latest" {
		return version, nil
	}
	url := registry + "/main/index.json"
	resp, err := http.Get(url)
	if err != nil || resp.StatusCode != 200 {
		return "", fmt.Errorf("cannot resolve 'latest' for %s: fetch index.json failed — specify an explicit version (e.g. \"main\" or \"v1.0.0\")", name)
	}
	defer resp.Body.Close()
	var idx RegistryIndex
	if err := json.NewDecoder(resp.Body).Decode(&idx); err != nil {
		return "", fmt.Errorf("invalid registry index: %w", err)
	}
	entry, ok := idx.Packages[name]
	if !ok || entry.Latest == "" {
		return "", fmt.Errorf("package %q not listed in registry index", name)
	}
	return entry.Latest, nil
}

func fetchPackageManifest(registry, name, version string) (*PackageManifest, error) {
	// URL: {registry}/{git-ref}/{package}/package.json
	url := fmt.Sprintf("%s/%s/%s/package.json", registry, version, name)
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("network error for %s@%s: %w", name, version, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return nil, fmt.Errorf("package %s@%s not found (tried: %s)", name, version, url)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("registry returned HTTP %d for %s@%s", resp.StatusCode, name, version)
	}
	var m PackageManifest
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return nil, fmt.Errorf("invalid manifest for %s@%s: %w", name, version, err)
	}
	return &m, nil
}

func downloadFile(url, destPath string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, url)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(destPath, data, 0644); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(data)), nil
}

// ---- commands ----

// cmdInit creates a starter esh.json.
func cmdInit() error {
	if _, err := os.Stat("esh.json"); err == nil {
		return fmt.Errorf("esh.json already exists")
	}
	wd, _ := os.Getwd()
	cfg := &EshConfig{
		Name:    filepath.Base(wd),
		Version: "1.0.0",
		Require: map[string]string{},
	}
	if err := writeEshConfig(cfg); err != nil {
		return err
	}
	fmt.Println("Created esh.json")
	return nil
}

// cmdInstall installs all packages in esh.json (with optional package spec to add one first).
func cmdInstall(args []string) error {
	cfg, err := readEshConfig()
	if err != nil {
		return err
	}

	// If a package spec was supplied, add it to esh.json before installing.
	if len(args) > 0 {
		name, ver := parsePackageSpec(args)
		cfg.Require[name] = ver
		if err := writeEshConfig(cfg); err != nil {
			return fmt.Errorf("write esh.json: %w", err)
		}
		fmt.Printf("Added %s@%s to esh.json\n\n", name, ver)
	}

	if len(cfg.Require) == 0 {
		fmt.Println("Nothing to install.")
		return nil
	}

	registry := registryBase(cfg)

	if err := os.MkdirAll("esh_vendors", 0755); err != nil {
		return fmt.Errorf("cannot create esh_vendors: %w", err)
	}

	lock := loadLock()
	installed := 0

	fmt.Printf("Installing %d package(s)...\n\n", len(cfg.Require))

	for name, version := range cfg.Require {
		resolved, err := resolveVersion(registry, name, version)
		if err != nil {
			return err
		}

		if locked, ok := lock.Packages[name]; ok && locked.Version == resolved {
			fmt.Printf("  ok   %s@%s (already installed)\n", name, resolved)
			continue
		}

		fmt.Printf("  ->   %s@%s\n", name, resolved)

		manifest, err := fetchPackageManifest(registry, name, resolved)
		if err != nil {
			return err
		}

		locked := LockedPackage{Version: resolved, Files: make(map[string]string)}
		for _, file := range manifest.Files {
			// URL: {registry}/{git-ref}/{package}/{file}
			url := fmt.Sprintf("%s/%s/%s/%s", registry, resolved, name, file)
			dest := filepath.Join("esh_vendors", file)
			fmt.Printf("       %s\n", file)
			hash, err := downloadFile(url, dest)
			if err != nil {
				return fmt.Errorf("download %s: %w", file, err)
			}
			locked.Files[file] = hash
		}

		lock.Packages[name] = locked
		installed++
		fmt.Printf("  ok   %s@%s installed\n", name, resolved)
	}

	if err := saveLock(lock); err != nil {
		return fmt.Errorf("write esh.lock: %w", err)
	}

	fmt.Printf("\n%d package(s) installed.\n", installed)
	return buildAndTest()
}

// cmdRemove removes a package's files and rebuilds.
func cmdRemove(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: esh remove <package>")
	}
	name := args[0]

	cfg, err := readEshConfig()
	if err != nil {
		return err
	}
	if _, ok := cfg.Require[name]; !ok {
		return fmt.Errorf("package %q not in esh.json", name)
	}

	lock := loadLock()
	if locked, ok := lock.Packages[name]; ok {
		for file := range locked.Files {
			path := filepath.Join("esh_vendors", file)
			if err := os.Remove(path); err == nil {
				fmt.Printf("  removed %s\n", path)
			}
		}
		delete(lock.Packages, name)
		saveLock(lock)
	}

	delete(cfg.Require, name)
	if err := writeEshConfig(cfg); err != nil {
		return err
	}
	fmt.Printf("Removed %s from esh.json\n\n", name)
	return buildAndTest()
}

// cmdPackages lists installed packages from esh.lock.
func cmdPackages() error {
	lock := loadLock()
	if len(lock.Packages) == 0 {
		fmt.Println("No packages installed.")
		return nil
	}
	fmt.Printf("\n  %-30s  %s\n", "Package", "Version")
	fmt.Printf("  %-30s  %s\n", strings.Repeat("-", 30), strings.Repeat("-", 10))
	for name, pkg := range lock.Packages {
		fmt.Printf("  %-30s  %s\n", name, pkg.Version)
	}
	fmt.Println()
	return nil
}

// ---- build & test ----

func buildAndTest() error {
	fmt.Println("\nBuilding esh...")
	if err := runCmd("go", "build", "-o", "esh", "."); err != nil {
		return fmt.Errorf("build failed: %w", err)
	}
	fmt.Println("Build OK")
	fmt.Println()

	fmt.Println("Running tests...")
	if err := runCmd("go", "test", "./..."); err != nil {
		fmt.Fprintln(os.Stderr, "warning: tests failed")
	} else {
		fmt.Println("Tests OK")
	}
	return nil
}

func runCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// ---- helpers ----

// parsePackageSpec parses "name@version" or "name version" from CLI args.
func parsePackageSpec(args []string) (name, version string) {
	spec := args[0]
	if i := strings.LastIndex(spec, "@"); i > 0 {
		return spec[:i], spec[i+1:]
	}
	if len(args) >= 2 {
		return spec, args[1]
	}
	return spec, "latest"
}
