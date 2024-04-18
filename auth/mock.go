package auth

import (
	"fmt"
	"net/http"
	"strconv"
)

type MockClient struct {
	Client
}

func (c *MockClient) Auth(r *http.Request) (int32, error) {
	var uidStr string

	if c, err := r.Cookie("x-uid"); err == nil {
		uidStr = c.Value
	}

	if uidStr == "" {
		return 0, fmt.Errorf("empty x-uid or x-token from cookie")
	}
	uid, err := strconv.ParseInt(uidStr, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("error parse x-uid as integer: %v", err)
	}
	return int32(uid), nil
}
