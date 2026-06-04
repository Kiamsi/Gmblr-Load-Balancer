package health

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"poker-lb/pkg/pool"
)

// the json body returned by GET /api/poker/health
type HealthBody struct {
	BoxID      string  `json:"box_id"`
	UptimeS    float64 `json:"uptime_s"`
	TableCount int     `json:"table_count"`
}

// runs health checks against all servers on set intervals
type Prober struct {
	pool     *pool.Pool
	path     string
	interval time.Duration
	client   *http.Client
}

// creates a prober, with a 5 second timeout
func New(pool *pool.Pool, path string, intervalS, _, _ int) *Prober {

	var prober Prober

	prober.pool = pool
	prober.path = path
	prober.interval = time.Duration(intervalS) * time.Second
	prober.client = &http.Client{Timeout: 5 * time.Second} //hardcoded value of 5, but can be changed easily

	return &prober
}

// structures the logging for the health checker into standard json
func (prober *Prober) log(event, backend string, fields map[string]any) {

	entry := map[string]any{
		"ts":      time.Now().UTC().Format(time.RFC3339),
		"event":   event,
		"backend": backend,
	}

	for key, value := range fields {
		entry[key] = value
	}

	data, _ := json.Marshal(entry)
	fmt.Println(string(data))
}

/*
the function probe performs one health check against address and updates the pool.

 1. builds the full URL to probe
 2. puts in a get request and:
    counts a fail if an error is encountered
    immediately takes down if it returns code 503
 3. if the check is successful, tries to decode the json body into HealthBody and counts a success
*/
func (prober *Prober) probe(address string) {

	url := fmt.Sprintf("http://%s%s", address, prober.path) //step 1

	response, err := prober.client.Get(url) //step 2

	if err != nil {
		prober.log("health_probe_failed", address, map[string]any{"reason": "connection error: " + err.Error()})
		prober.pool.LogFail(address, err.Error())
		return
	}

	defer response.Body.Close()

	//this if clause triggers the drain protocol
	if response.StatusCode == http.StatusServiceUnavailable {
		prober.log("health_probe_failed", address, map[string]any{"reason": "code 503 — backend is draining or unhealthy"})
		prober.pool.LogFailInstant(address, "503 from health endpoint")
		return
	}

	//the rest can be else if statements but it doesn't matter
	if response.StatusCode != http.StatusOK {
		reason := fmt.Sprintf("unexpected status %d", response.StatusCode)
		prober.log("health_probe_failed", address, map[string]any{"reason": reason})
		prober.pool.LogFail(address, reason)
		return
	}

	var healthData HealthBody
	responseDecoder := json.NewDecoder(response.Body)
	decodingError := responseDecoder.Decode(&healthData)

	if decodingError == nil {
		logData := map[string]any{
			"box_id":      healthData.BoxID,
			"uptime_s":    healthData.UptimeS,
			"table_count": healthData.TableCount,
		}
		prober.log("health_probe_ok", address, logData)
	} else {
		warning := map[string]any{
			"reason": "encountered logging error - but backend server is fine",
			"error":  decodingError.Error(),
		}
		prober.log("health_probe_warning", address, warning)
	}

	prober.pool.LogSuccess(address) //3
}
