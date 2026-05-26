package main

import (
	"encoding/json"
	"testing"
)

func TestBuildDeploymentMatrix_singleFunctionName(t *testing.T) {
	t.Parallel()
	cfg := DeploymentConfig{
		FunctionName: "MyLambda",
		Targets: []TargetConfig{
			{Name: "us-east-1", Alias: "production"},
		},
	}
	got, err := BuildDeploymentMatrix(cfg, "my-lambda")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("len=%d want 1", len(got))
	}
	if got[0].FunctionName != "MyLambda" || got[0].LambdaID != "my-lambda" {
		t.Fatalf("row: %+v", got[0])
	}
}

func TestBuildDeploymentMatrix_multipleFunctionNames(t *testing.T) {
	t.Parallel()
	cfg := DeploymentConfig{
		FunctionNames: []string{"A", "B", "C"},
		Targets: []TargetConfig{
			{Name: "us-east-1", Alias: "production"},
		},
	}
	got, err := BuildDeploymentMatrix(cfg, "bnet-polling-function")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("len=%d want 3", len(got))
	}
	for i, want := range []string{"A", "B", "C"} {
		if got[i].FunctionName != want {
			t.Fatalf("[%d] function_name=%q want %q", i, got[i].FunctionName, want)
		}
		if got[i].LambdaID != "bnet-polling-function" {
			t.Fatalf("[%d] lambda_id=%q", i, got[i].LambdaID)
		}
		if got[i].Name != "us-east-1" {
			t.Fatalf("[%d] region=%q", i, got[i].Name)
		}
	}
}

func TestBuildDeploymentMatrix_rejectsBothNameFields(t *testing.T) {
	t.Parallel()
	_, err := BuildDeploymentMatrix(DeploymentConfig{
		FunctionName:  "One",
		FunctionNames: []string{"Two"},
		Targets:       []TargetConfig{{Name: "us-east-1"}},
	}, "id")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBuildDeploymentMatrix_rejectsEmptyFunctionName(t *testing.T) {
	t.Parallel()
	_, err := BuildDeploymentMatrix(DeploymentConfig{
		FunctionName: "  ",
		Targets:      []TargetConfig{{Name: "us-east-1"}},
	}, "id")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBuildDeploymentMatrix_rejectsEmptyFunctionNamesEntry(t *testing.T) {
	t.Parallel()
	_, err := BuildDeploymentMatrix(DeploymentConfig{
		FunctionNames: []string{"A", ""},
		Targets:       []TargetConfig{{Name: "us-east-1"}},
	}, "id")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBuildDeploymentMatrix_requiresFunctionName(t *testing.T) {
	t.Parallel()
	_, err := BuildDeploymentMatrix(DeploymentConfig{
		Targets: []TargetConfig{{Name: "us-east-1"}},
	}, "id")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBuildDeploymentMatrix_jsonRoundTrip(t *testing.T) {
	t.Parallel()
	got, err := BuildDeploymentMatrix(DeploymentConfig{
		FunctionNames: []string{"BNetPollingLambda", "BNetPollingLambdaEU"},
		Targets:       []TargetConfig{{Name: "us-east-1", Alias: "production"}},
	}, "bnet-polling-function")
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var decoded []TargetConfig
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded) != 2 || decoded[1].FunctionName != "BNetPollingLambdaEU" {
		t.Fatalf("decoded: %+v", decoded)
	}
}
