// /config_reader.go - Temporary script for CI

package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// RegionConfig maps to a single entry in the 'regions' list
type RegionConfig struct {
	Name  string `yaml:"name" json:"name"`
	Alias string `yaml:"alias" json:"alias"`
	// Note: FunctionName is *not* in the YAML region list, but we add it in Go before JSON output
	FunctionName string `yaml:"-" json:"function_name"`
}

// Config maps to the root of the YAML file
type Config struct {
	FunctionName string         `yaml:"function_name" json:"-"` // Root function name
	Regions      []RegionConfig `yaml:"regions" json:"-"`       // List of regions
}

func main() {
	configPath := filepath.Join("cmd", "bnet-polling-function", "deployment-config.yml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		log.Fatalf("failed to read config file %s: %v", configPath, err)
	}

	var cfg Config
	err = yaml.Unmarshal(data, &cfg)
	if err != nil {
		log.Fatalf("failed to unmarshal YAML: %v", err)
	}

	// 1. Inject the single FunctionName into every RegionConfig item
	var deploymentMatrix []RegionConfig
	for _, region := range cfg.Regions {
		region.FunctionName = cfg.FunctionName // Inject the root function_name
		deploymentMatrix = append(deploymentMatrix, region)
	}

	// 2. Output the combined array as a single-line JSON string
	jsonOutput, err := json.Marshal(deploymentMatrix)
	if err != nil {
		log.Fatalf("failed to marshal to JSON: %v", err)
	}

	os.Stdout.Write(jsonOutput)
}
