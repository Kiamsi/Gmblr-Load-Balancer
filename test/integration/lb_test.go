package integration

import (
	"net/http/httptest"
	"sync"
)

// mock for testing
type fakeBackend struct {
	server   *httptest.Server
	mu       sync.Mutex
	healthy  bool
	draining bool
}
