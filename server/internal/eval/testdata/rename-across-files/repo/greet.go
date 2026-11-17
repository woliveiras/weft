package users

// Greeting greets u by name.
func Greeting(u User) string {
	return "Hello, " + GetUserName(u) + "!"
}
