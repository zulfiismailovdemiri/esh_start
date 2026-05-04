// Bootstrap is a self-contained installer that runs without esh_vendors/ existing.
// Use it once after cloning esh_start to pull packages and build the esh binary:
//
//	go run ./bootstrap/
//
// After that, use ./esh install for all future package operations.
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

const defaultRegistry = "https://raw.githubusercontent.com/zulfiismailovdemiri/esh_repo"

type EshConfig struct {
	Name     string            `json:"name"`
	Version  string            `json:"version"`
	Require  map[string]string `json:"require"`
	Registry string            `json:"registry,omitempty"`
}

type EshLock struct {
	Packages map[string]LockedPackage `json:"packages"`
}

type LockedPackage struct {
	Version string            `json:"version"`
	Files   map[string]string `json:"files"`
}

type PackageManifest struct {
	Name    string   `json:"name"`
	Version string   `json:"version"`
	Files   []string `json:"files"`
}

type RegistryIndex struct {
	Packages map[string]RegistryEntry `json:"packages"`
}

type RegistryEntry struct {
	Latest string `json:"latest"`
}

func main() {
	if err := install(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func install() error {
	data, err := os.ReadFile("esh.json")
	if err != nil {
		return fmt.Errorf("esh.json not found — are you in the project root?")
	}
	var cfg EshConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("invalid esh.json: %w", err)
	}
	if len(cfg.Require) == 0 {
		fmt.Println("Nothing to install.")
		return nil
	}

	registry := defaultRegistry
	if cfg.Registry != "" {
		registry = strings.TrimRight(cfg.Registry, "/")
	}

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

		manifest, err := fetchManifest(registry, name, resolved)
		if err != nil {
			return err
		}

		locked := LockedPackage{Version: resolved, Files: make(map[string]string)}
		for _, file := range manifest.Files {
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

	fmt.Printf("\n%d package(s) installed.\n\nBuilding esh...\n", installed)

	cmd := exec.Command("go", "build", "-o", "esh", ".")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("build failed: %w", err)
	}
	fmt.Println("Build OK")
	return nil
}

func resolveVersion(registry, name, version string) (string, error) {
	if version != "latest" {
		return version, nil
	}
	resp, err := http.Get(registry + "/main/index.json")
	if err != nil || resp.StatusCode != 200 {
		return "", fmt.Errorf("cannot resolve 'latest' for %s: specify an explicit version (e.g. \"main\")", name)
	}
	defer resp.Body.Close()
	var idx RegistryIndex
	if err := json.NewDecoder(resp.Body).Decode(&idx); err != nil {
		return "", fmt.Errorf("invalid registry index: %w", err)
	}
	entry, ok := idx.Packages[name]
	if !ok || entry.Latest == "" {
		return "", fmt.Errorf("package %q not in registry index", name)
	}
	return entry.Latest, nil
}

func fetchManifest(registry, name, version string) (*PackageManifest, error) {
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
		return nil, fmt.Errorf("HTTP %d for %s@%s", resp.StatusCode, name, version)
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
