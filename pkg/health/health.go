package health

import (
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
