package users

import "strings"

// Report lists the names of users, one per line.
func Report(us []User) string {
	names := make([]string, 0, len(us))
	for _, u := range us {
		names = append(names, GetUserName(u))
	}
	return strings.Join(names, "\n")
}
