package main // Must be 'package main' to be executable via 'go run'

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// RegionConfig maps to a single entry in the 'regions' list
type RegionConfig struct {
	Name         string `yaml:"name" json:"name"`
	Alias        string `yaml:"alias" json:"alias"`
	FunctionName string `yaml:"-" json:"function_name"` // Injected from root of YAML
}

// Config maps to the root of the YAML file
type Config struct {
	FunctionName string         `yaml:"function_name" json:"-"` // Central function name
	Regions      []RegionConfig `yaml:"regions" json:"-"`
}

func main() {
	// Path to YAML remains relative to the repository root (where the CI runs 'go run')
	configPath := filepath.Join("cmd", "bnet-polling-function", "deployment_config.yml")

	data, err := os.ReadFile(configPath)
	if err != nil {
		log.Fatalf("failed to read config file %s: %v", configPath, err)
	}

	var cfg Config
	err = yaml.Unmarshal(data, &cfg)
	if err != nil {
		log.Fatalf("failed to unmarshal YAML: %v", err)
	}

	// Inject the single FunctionName into every RegionConfig item
	var deploymentMatrix []RegionConfig
	for _, region := range cfg.Regions {
		region.FunctionName = cfg.FunctionName
		deploymentMatrix = append(deploymentMatrix, region)
	}

	// Output the combined array as a single-line JSON string
	jsonOutput, err := json.Marshal(deploymentMatrix)
	if err != nil {
		log.Fatalf("failed to marshal to JSON: %v", err)
	}

	os.Stdout.Write(jsonOutput)
}
