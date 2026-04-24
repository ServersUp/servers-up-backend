package main

import (
	"encoding/json"
	"log/slog"
	"os"

	"gopkg.in/yaml.v3"
)

// TargetConfig represents a generic deployment target in a multi-region setup.
type TargetConfig struct {
	Name         string         `yaml:"name" json:"name"`
	Alias        string         `yaml:"alias" json:"alias"`
	FunctionName string         `yaml:"-" json:"function_name"` // Injected from the root configuration
	Meta         map[string]any `yaml:"meta,omitempty" json:"meta,omitempty"`
}

// DeploymentConfig defines the schema for the multi-region deployment manifest.
type DeploymentConfig struct {
	FunctionName string         `yaml:"function_name" json:"-"`
	Targets      []TargetConfig `yaml:"regions" json:"-"` // Using 'regions' as the YAML key for backward compatibility
}

func main() {
	if len(os.Args) < 2 {
		slog.Error("usage: config-reader <config-path>")
		os.Exit(1)
	}

	configPath := os.Args[1]
	data, err := os.ReadFile(configPath)
	if err != nil {
		slog.Error("failed to read config file", "path", configPath, "error", err)
		os.Exit(1)
	}

	var cfg DeploymentConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		slog.Error("failed to unmarshal YAML", "error", err)
		os.Exit(1)
	}

	// Build the deployment matrix by injecting global properties into each target.
	var deploymentMatrix []TargetConfig
	for _, target := range cfg.Targets {
		target.FunctionName = cfg.FunctionName
		deploymentMatrix = append(deploymentMatrix, target)
	}

	jsonOutput, err := json.Marshal(deploymentMatrix)
	if err != nil {
		slog.Error("failed to marshal to JSON", "error", err)
		os.Exit(1)
	}

	os.Stdout.Write(jsonOutput)
}
