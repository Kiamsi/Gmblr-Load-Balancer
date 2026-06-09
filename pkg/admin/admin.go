package admin

import (
	"net/http"
	"poker-lb/pkg/pool"
)

type Server struct {
	pool                *pool.Pool
	authenticationToken string
	mux                 http.ServeMux
}

func NewAdmin(pool *pool.Pool, authenticationToken string)
