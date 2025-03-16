package api

import "fmt"

// Error represents an error returned by the Telegram API.
type Error struct {
	StatusCode int
	Desc       string
}

// ErrorCode returns the error code received from the Telegram API.
func (e Error) ErrorCode() int {
	return e.StatusCode
}

// Description returns the error description received from the Telegram API.
func (e Error) Description() string {
	return e.Desc
}

// Error returns the error string.
func (e Error) Error() string {
	return fmt.Sprintf("API error %d: %s", e.StatusCode, e.Desc)
}
