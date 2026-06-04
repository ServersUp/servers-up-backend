package metrics

import (
	"encoding/json"
	"io"
	"os"
	"slices"
	"time"
)

// Namespace is the CloudWatch metrics namespace for ServersUp Lambdas.
// Keep custom metric count low (free tier ~10); do not add metrics without review.
const Namespace = "ServersUp"

// EmitCount writes an Embedded Metric Format (EMF) JSON line to stdout.
// CloudWatch can extract metrics from these log lines without a separate agent.
//
// Keep dimensions low-cardinality (e.g. gameId) to control cost.
func EmitCount(namespace, metricName string, dimensions map[string]string, value int64) {
	emitTo(os.Stdout, namespace, metricName, "Count", dimensions, value)
}

// emitTo is the implementation used by EmitCount and by tests.
func emitTo(w io.Writer, namespace, metricName, unit string, dimensions map[string]string, value int64) {
	if namespace == "" || metricName == "" {
		return
	}

	dimKeys := make([]string, 0, len(dimensions))
	for k := range dimensions {
		dimKeys = append(dimKeys, k)
	}
	slices.Sort(dimKeys)

	root := map[string]any{
		"_aws": map[string]any{
			"Timestamp": time.Now().UnixMilli(),
			"CloudWatchMetrics": []any{
				map[string]any{
					"Namespace":  namespace,
					"Dimensions": [][]string{dimKeys},
					"Metrics": []any{
						map[string]any{
							"Name": metricName,
							"Unit": unit,
						},
					},
				},
			},
		},
		metricName: value,
	}

	for k, v := range dimensions {
		root[k] = v
	}

	b, err := json.Marshal(root)
	if err != nil {
		return
	}
	_, _ = w.Write(append(b, '\n'))
}
