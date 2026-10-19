package iptv

import "errors"

var ErrUserNotFound = errors.New("user not found")

// User holds the data needed to authenticate a user. It is not an API response.
type User struct {
	ID           int64
	PasswordHash string
	Active       bool
}
