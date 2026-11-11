// Package users manages user records.
package users

// User is an account.
type User struct {
	First, Last string
}

// GetUserName returns the display name of u.
func GetUserName(u User) string {
	return u.First + " " + u.Last
}
