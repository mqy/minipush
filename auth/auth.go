package auth

import "net/http"

type Client interface {
	// Auth authenticate current user, return uid.
	Auth(r *http.Request) (int32, error)
}
