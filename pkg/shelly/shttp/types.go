package http

type Response struct {
	// Code of the HTTP response
	Code int `json:"code"`
	// Message of the HTTP response
	Message string `json:"message"`
	// List of HTTP headers sent by the server
	Headers map[string]string `json:"headers"`
	// HTTP response body, if Content-Type is text or application/json
	Body string `json:"body"`
	// base64 encoded HTTP response body, if body is binary data. Maximum accepted length is 16KB (16384 bytes)
	BodyBase64 string `json:"body_b64"`
}
