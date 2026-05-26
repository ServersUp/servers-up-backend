package main

import (
	"fmt"
	"strings"
)

// DeploymentConfig defines the schema for the multi-region deployment manifest.
type DeploymentConfig struct {
	FunctionName  string         `yaml:"function_name" json:"-"`
	FunctionNames []string       `yaml:"function_names" json:"-"`
	Targets       []TargetConfig `yaml:"regions" json:"-"` // Using 'regions' as the YAML key for backward compatibility
}

func (cfg DeploymentConfig) resolvedFunctionNames() ([]string, error) {
	hasSingle := cfg.FunctionName != ""
	hasMulti := len(cfg.FunctionNames) > 0
	if hasSingle && hasMulti {
		return nil, fmt.Errorf("deployment config: set function_name or function_names, not both")
	}
	if hasMulti {
		for i, name := range cfg.FunctionNames {
			if strings.TrimSpace(name) == "" {
				return nil, fmt.Errorf("deployment config: function_names[%d] must not be empty", i)
			}
		}
		return cfg.FunctionNames, nil
	}
	if hasSingle {
		if strings.TrimSpace(cfg.FunctionName) == "" {
			return nil, fmt.Errorf("deployment config: function_name must not be empty")
		}
		return []string{cfg.FunctionName}, nil
	}
	return nil, fmt.Errorf("deployment config: function_name or function_names is required")
}

// BuildDeploymentMatrix expands regions × function names into deploy matrix rows.
// Multiple function names share the same lambdaID (one build artifact, many update-function-code targets).
func BuildDeploymentMatrix(cfg DeploymentConfig, lambdaID string) ([]TargetConfig, error) {
	names, err := cfg.resolvedFunctionNames()
	if err != nil {
		return nil, err
	}
	if len(cfg.Targets) == 0 {
		return nil, fmt.Errorf("deployment config: at least one region is required")
	}

	var out []TargetConfig
	for _, target := range cfg.Targets {
		for _, name := range names {
			row := target
			row.FunctionName = name
			row.LambdaID = lambdaID
			out = append(out, row)
		}
	}
	return out, nil
}
