// Package models holds request/response DTOs shared by handlers.
package models

// APIResponse is the envelope on every response. Message is what a person
// reads; Error is a stable snake_case code the frontend branches on.
type APIResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
	Error   *string     `json:"error,omitempty"`
}

// PaginatedResponse wraps a list with paging metadata.
type PaginatedResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
	Meta    MetaData    `json:"meta"`
}

// MetaData is pagination metadata.
type MetaData struct {
	CurrentPage int `json:"current_page"`
	PerPage     int `json:"per_page"`
	Total       int `json:"total"`
	TotalPages  int `json:"total_pages"`
}

// StringPtr returns a pointer to s, for APIResponse.Error.
func StringPtr(s string) *string { return &s }

// Fail builds a failure envelope with a stable error code.
func Fail(message, code string) APIResponse {
	return APIResponse{Success: false, Message: message, Error: StringPtr(code)}
}

// OK builds a success envelope.
func OK(message string, data interface{}) APIResponse {
	return APIResponse{Success: true, Message: message, Data: data}
}
