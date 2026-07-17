package workspace

import "time"

const ExitStale = 4

type Command struct {
	Run         string `yaml:"run" json:"run"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	Snapshot    string `yaml:"snapshot,omitempty" json:"snapshot,omitempty"`
	Source      string `yaml:"-" json:"source,omitempty"`
}

type Manifest struct {
	Version  int                `yaml:"version"`
	Commands map[string]Command `yaml:"commands,omitempty"`
}

type Inspection struct {
	Root     string             `json:"root"`
	Name     string             `json:"name"`
	Git      bool               `json:"git"`
	Detected []string           `json:"detected"`
	Commands map[string]Command `json:"commands"`
}

type RunResult struct {
	Command      string `json:"command"`
	ExitCode     int    `json:"exit_code"`
	Stale        bool   `json:"stale"`
	TestedState  string `json:"tested_state,omitempty"`
	CurrentState string `json:"current_state,omitempty"`
}

type InstalledPackage struct {
	Name      string    `yaml:"name" json:"name"`
	Version   string    `yaml:"version" json:"version"`
	Source    string    `yaml:"source" json:"source"`
	Digest    string    `yaml:"digest" json:"digest"`
	Installed time.Time `yaml:"installed" json:"installed"`
}

type PackageManifest struct {
	Name     string             `yaml:"name"`
	Version  string             `yaml:"version"`
	Commands map[string]Command `yaml:"commands"`
}

type Lockfile struct {
	Version  int                         `yaml:"version"`
	Packages map[string]InstalledPackage `yaml:"packages"`
}
