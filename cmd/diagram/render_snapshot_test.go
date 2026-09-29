package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type renderSnapshot struct {
	Name          string   `json:"name"`
	Args          []string `json:"args"`
	DiagramSHA256 string   `json:"diagram_sha256"`
	DebugSHA256   string   `json:"debug_sha256"`
	Optimization  string   `json:"optimization"`
	Bounds        string   `json:"bounds"`
	RouteMetrics  string   `json:"route_metrics"`
}

func TestRenderSnapshots(t *testing.T) {
	snapshots := readRenderSnapshots(t)
	validateRenderSnapshotSet(t, snapshots)
	for _, snapshot := range snapshots {
		snapshot := snapshot
		t.Run(snapshot.Name, func(t *testing.T) {
			input, err := os.ReadFile(filepath.Join("testdata", "e2e", snapshot.Name+".puml"))
			if err != nil {
				t.Fatal(err)
			}
			diagram := runE2E(t, snapshot.Args, input)
			debugArgs := append(append([]string(nil), snapshot.Args...), "--debug-layout")
			debug := runE2E(t, debugArgs, input)
			if got := contentSHA256(diagram); got != snapshot.DiagramSHA256 {
				t.Fatalf("diagram hash = %s, want %s", got, snapshot.DiagramSHA256)
			}
			if got := contentSHA256(debug); got != snapshot.DebugSHA256 {
				t.Fatalf("debug hash = %s, want %s", got, snapshot.DebugSHA256)
			}
			assertDebugSnapshot(t, debug, snapshot)
		})
	}
}

func validateRenderSnapshotSet(t *testing.T, snapshots []renderSnapshot) {
	t.Helper()
	fixturePaths, err := filepath.Glob(filepath.Join("testdata", "e2e", "*.puml"))
	if err != nil {
		t.Fatal(err)
	}
	fixtures := make(map[string]bool, len(fixturePaths))
	for _, path := range fixturePaths {
		fixtures[strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))] = true
	}
	snapshotNames := make(map[string]bool, len(snapshots))
	for _, snapshot := range snapshots {
		if snapshotNames[snapshot.Name] {
			t.Fatalf("duplicate render snapshot %q", snapshot.Name)
		}
		snapshotNames[snapshot.Name] = true
		if !fixtures[snapshot.Name] {
			t.Errorf("render snapshot %q has no E2E fixture", snapshot.Name)
		}
	}
	for name := range fixtures {
		if !snapshotNames[name] {
			t.Errorf("E2E fixture %q has no render snapshot", name)
		}
	}
}

func TestRenderSnapshotHashAcrossProcesses(t *testing.T) {
	snapshot := renderSnapshotByName(t, readRenderSnapshots(t), "11-complex-optimized")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(executable, "-test.run=^TestRenderSnapshotHelper$")
	command.Env = append(os.Environ(), "DIAGRAM_SNAPSHOT_HELPER="+snapshot.Name)
	output, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	if got := contentSHA256(string(output)); got != snapshot.DebugSHA256 {
		t.Fatalf("subprocess debug hash = %s, want %s", got, snapshot.DebugSHA256)
	}
}

func TestRenderSnapshotHelper(t *testing.T) {
	name := os.Getenv("DIAGRAM_SNAPSHOT_HELPER")
	if name == "" {
		return
	}
	input, err := os.ReadFile(filepath.Join("testdata", "e2e", name+".puml"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := run([]string{"--debug-layout"}, bytes.NewReader(input), os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

func readRenderSnapshots(t *testing.T) []renderSnapshot {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "snapshots", "render.json"))
	if err != nil {
		t.Fatal(err)
	}
	var snapshots []renderSnapshot
	if err := json.Unmarshal(data, &snapshots); err != nil {
		t.Fatal(err)
	}
	return snapshots
}

func renderSnapshotByName(t *testing.T, snapshots []renderSnapshot, name string) renderSnapshot {
	t.Helper()
	for _, snapshot := range snapshots {
		if snapshot.Name == name {
			return snapshot
		}
	}
	t.Fatalf("render snapshot %q is missing", name)
	return renderSnapshot{}
}

func assertDebugSnapshot(t *testing.T, debug string, snapshot renderSnapshot) {
	t.Helper()
	for _, expected := range []string{
		"optimization: " + snapshot.Optimization,
		"bounds: " + snapshot.Bounds,
		"route_metrics: " + snapshot.RouteMetrics,
	} {
		if !strings.Contains(debug, expected+"\n") {
			t.Fatalf("debug output is missing %q", expected)
		}
	}
}

func contentSHA256(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}
