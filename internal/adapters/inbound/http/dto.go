package http

// Request/response DTOs for the HTTP transport. They exist so the wire format
// is decoupled from both the application command objects and the domain.
type registerRequest struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

type renameRequest struct {
	Name string `json:"name"`
}

type errorResponse struct {
	Error string `json:"error"`
}
