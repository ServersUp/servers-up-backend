package common

// User represents a shared domain model.
type User struct {
    ID    string
    Email string
}

// GetData is a placeholder for shared business logic.
func GetData(id string) (User, error) {
    // Logic to fetch user data, e.g., from a database
    return User{ID: id, Email: "user@example.com"}, nil
}
