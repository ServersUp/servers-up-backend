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
	LambdaID     string         `yaml:"-" json:"lambda_id"`     // Injected for matrix identification
	Meta         map[string]any `yaml:"meta,omitempty" json:"meta,omitempty"`
}

func main() {
	if len(os.Args) < 2 {
		slog.Error("usage: config-reader <config-path> [lambda-id]")
		os.Exit(1)
	}

	configPath := os.Args[1]
	lambdaID := ""
	if len(os.Args) > 2 {
		lambdaID = os.Args[2]
	}
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

	deploymentMatrix, err := BuildDeploymentMatrix(cfg, lambdaID)
	if err != nil {
		slog.Error("failed to build deployment matrix", "error", err)
		os.Exit(1)
	}

	jsonOutput, err := json.Marshal(deploymentMatrix)
	if err != nil {
		slog.Error("failed to marshal to JSON", "error", err)
		os.Exit(1)
	}

	os.Stdout.Write(jsonOutput)
}
