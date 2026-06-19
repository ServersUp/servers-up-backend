package metrics

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

func TestEmitCount_jsonShape(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	emitTo(&buf, "ServersUp", "PollRealmSuccess", "Count", map[string]string{"gameId": "wow", "bnetRegion": "us"}, 3)

	line := bytes.TrimRight(buf.Bytes(), "\n")
	if len(line) == 0 {
		t.Fatal("expected non-empty EMF output")
	}

	var root map[string]any
	if err := json.Unmarshal(line, &root); err != nil {
		t.Fatalf("invalid JSON: %v\nraw: %s", err, line)
	}

	// Top-level metric value.
	if v, ok := root["PollRealmSuccess"]; !ok || v == nil {
		t.Errorf("expected PollRealmSuccess key, got %v", root)
	}
	if root["gameId"] != "wow" || root["bnetRegion"] != "us" {
		t.Errorf("expected dimension keys at root level, got %v", root)
	}

	// _aws envelope.
	awsRaw, ok := root["_aws"].(map[string]any)
	if !ok {
		t.Fatalf("expected _aws map, got %T", root["_aws"])
	}
	if awsRaw["Timestamp"] == nil {
		t.Error("expected _aws.Timestamp")
	}
	cwmRaw, ok := awsRaw["CloudWatchMetrics"].([]any)
	if !ok || len(cwmRaw) == 0 {
		t.Fatalf("expected non-empty CloudWatchMetrics, got %v", awsRaw["CloudWatchMetrics"])
	}
	entry, ok := cwmRaw[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected CloudWatchMetrics entry type %T", cwmRaw[0])
	}
	if entry["Namespace"] != "ServersUp" {
		t.Errorf("expected Namespace=ServersUp, got %v", entry["Namespace"])
	}
	metrics, ok := entry["Metrics"].([]any)
	if !ok || len(metrics) == 0 {
		t.Fatalf("expected Metrics array, got %v", entry["Metrics"])
	}
	m, _ := metrics[0].(map[string]any)
	if m["Name"] != "PollRealmSuccess" || m["Unit"] != "Count" {
		t.Errorf("unexpected metric entry: %v", m)
	}
	dims, ok := entry["Dimensions"].([]any)
	if !ok || len(dims) == 0 {
		t.Fatalf("expected Dimensions array, got %v", entry["Dimensions"])
	}
	dimRow, ok := dims[0].([]any)
	if !ok {
		t.Fatalf("expected dimension name row, got %T", dims[0])
	}
	if len(dimRow) != 2 {
		t.Fatalf("expected 2 dimension keys, got %v", dimRow)
	}
	if dimRow[0] != "bnetRegion" || dimRow[1] != "gameId" {
		t.Errorf("expected sorted dimension keys [bnetRegion gameId], got %v", dimRow)
	}
}

func TestEmitCount_dimensionOrderDeterministic(t *testing.T) {
	t.Parallel()

	dims := map[string]string{"gameId": "wow", "bnetRegion": "us", "zebra": "z"}
	want := []string{"bnetRegion", "gameId", "zebra"}

	for i := 0; i < 3; i++ {
		var buf bytes.Buffer
		emitTo(&buf, "ServersUp", "M", "Count", dims, 1)
		got := dimensionNamesFromEMF(t, buf.Bytes())
		if len(got) != len(want) {
			t.Fatalf("emit %d: got %d keys %v, want %v", i, len(got), got, want)
		}
		for j := range want {
			if got[j] != want[j] {
				t.Fatalf("emit %d: index %d got %q want %q (row %v)", i, j, got[j], want[j], got)
			}
		}
	}
}

func dimensionNamesFromEMF(t *testing.T, raw []byte) []string {
	t.Helper()
	line := bytes.TrimRight(raw, "\n")
	var root map[string]any
	if err := json.Unmarshal(line, &root); err != nil {
		t.Fatal(err)
	}
	awsRaw, ok := root["_aws"].(map[string]any)
	if !ok {
		t.Fatal("missing _aws")
	}
	cwm, ok := awsRaw["CloudWatchMetrics"].([]any)
	if !ok || len(cwm) == 0 {
		t.Fatal("missing CloudWatchMetrics")
	}
	entry, ok := cwm[0].(map[string]any)
	if !ok {
		t.Fatal("bad CloudWatchMetrics entry")
	}
	dims, ok := entry["Dimensions"].([]any)
	if !ok || len(dims) == 0 {
		t.Fatal("missing Dimensions")
	}
	row, ok := dims[0].([]any)
	if !ok {
		t.Fatal("bad dimension row")
	}
	out := make([]string, len(row))
	for i, v := range row {
		s, ok := v.(string)
		if !ok {
			t.Fatalf("dimension %d not string: %T", i, v)
		}
		out[i] = s
	}
	return out
}

func TestEmitCount_noopOnEmptyInputs(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	emitTo(&buf, "", "metric", "Count", nil, 1)
	emitTo(&buf, "ns", "", "Count", nil, 1)

	if buf.Len() != 0 {
		t.Errorf("expected no output for empty namespace/metric, got %q", buf.String())
	}
}

func TestEmitCount_writesToStdout(t *testing.T) {
	// Smoke: EmitCount must not panic writing to os.Stdout.
	// We redirect stdout only to verify there's no panic, not to capture content.
	EmitCount("ServersUp", "TestMetric", map[string]string{"gameId": "test"}, 1)
	_ = os.Stdout
}

func TestEmitMilliseconds_jsonShape(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	emitTo(&buf, "ServersUp", "PollDurationMs", "Milliseconds", map[string]string{"gameId": "wow", "bnetRegion": "us"}, 14000)

	var root map[string]any
	if err := json.Unmarshal(bytes.TrimRight(buf.Bytes(), "\n"), &root); err != nil {
		t.Fatal(err)
	}
	if root["PollDurationMs"] != float64(14000) {
		t.Errorf("PollDurationMs value: %v", root["PollDurationMs"])
	}
	cwm := root["_aws"].(map[string]any)["CloudWatchMetrics"].([]any)[0].(map[string]any)
	metric := cwm["Metrics"].([]any)[0].(map[string]any)
	if metric["Unit"] != "Milliseconds" {
		t.Errorf("unit: %v", metric["Unit"])
	}
}
