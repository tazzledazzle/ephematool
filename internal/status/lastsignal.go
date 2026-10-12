package status

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ephematool/ephematool/internal/telemetry"
)

// NewLastSignal returns a LastSignal check that queries Prometheus with the
// constant PromQL from internal/telemetry. baseURL comes from server config only.
func NewLastSignal(baseURL string, timeout time.Duration) func(context.Context) (map[string]time.Time, error) {
	// Plain client on purpose (no tracing transport), so queries create no traces.
	client := &http.Client{Timeout: timeout}
	base := strings.TrimRight(baseURL, "/")
	return func(ctx context.Context) (map[string]time.Time, error) {
		out := map[string]time.Time{}
		for _, q := range []string{telemetry.LastSignalQuery, telemetry.DashboardLastSignalQuery} {
			if err := queryVector(ctx, client, base, q, out); err != nil {
				return nil, err
			}
		}
		return out, nil
	}
}

type promResponse struct {
	Status string `json:"status"`
	Data   struct {
		Result []struct {
			Metric map[string]string `json:"metric"`
			Value  [2]any            `json:"value"`
		} `json:"result"`
	} `json:"data"`
}

func queryVector(ctx context.Context, client *http.Client, base, query string, out map[string]time.Time) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		base+"/api/v1/query?query="+url.QueryEscape(query), nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("prometheus query: HTTP %d", resp.StatusCode)
	}
	var body promResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return err
	}
	if body.Status != "success" {
		return fmt.Errorf("prometheus query status %q", body.Status)
	}
	for _, r := range body.Data.Result {
		name := r.Metric[telemetry.LabelServiceName]
		s, ok := r.Value[1].(string)
		if name == "" || !ok {
			continue
		}
		secs, err := strconv.ParseFloat(s, 64)
		if err != nil {
			continue
		}
		ts := time.Unix(0, int64(secs*float64(time.Second))).UTC()
		if cur, seen := out[name]; !seen || ts.After(cur) {
			out[name] = ts
		}
	}
	return nil
}
