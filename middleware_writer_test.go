package substrate

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStatusCodeResponseWriter(t *testing.T) {
	tests := []struct {
		name           string
		statusCode     int
		content        string
		expectedStatus int
		expectedBody   string
	}{
		{
			name:           "Normal status code",
			statusCode:     http.StatusOK,
			content:        "Hello, world!",
			expectedStatus: http.StatusOK,
			expectedBody:   "Hello, world!",
		},
		{
			name:           "Error status code",
			statusCode:     http.StatusInternalServerError,
			content:        "Internal Server Error",
			expectedStatus: http.StatusInternalServerError,
			expectedBody:   "Internal Server Error",
		},
		{
			name:           "Status 515",
			statusCode:     515,
			content:        "This content should be discarded",
			expectedStatus: 515,
			expectedBody:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a response recorder
			rec := httptest.NewRecorder()
			
			// Create our custom writer
			writer := &statusCodeResponseWriter{ResponseWriter: rec}
			
			// Write the status code and content
			writer.WriteHeader(tt.statusCode)
			writer.Write([]byte(tt.content))
			
			// Check the status code
			if writer.Status() != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, writer.Status())
			}
			
			// Check the response body
			if rec.Body.String() != tt.expectedBody {
				t.Errorf("Expected body %q, got %q", tt.expectedBody, rec.Body.String())
			}
		})
	}
}

func TestStatusCodeResponseWriterDefaultStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	writer := &statusCodeResponseWriter{ResponseWriter: rec}
	
	// Write without setting status code
	writer.Write([]byte("Hello"))
	
	// Status should default to 200 OK
	if writer.Status() != http.StatusOK {
		t.Errorf("Expected default status %d, got %d", http.StatusOK, writer.Status())
	}
}

func TestStatusCodeResponseWriterImplementsInterfaces(t *testing.T) {
	rec := httptest.NewRecorder()
	writer := &statusCodeResponseWriter{ResponseWriter: rec}
	
	// Test http.Flusher interface
	_, isFlusher := interface{}(writer).(http.Flusher)
	if !isFlusher {
		t.Error("Writer does not implement http.Flusher")
	}
	
	// Test http.Hijacker interface
	_, isHijacker := interface{}(writer).(http.Hijacker)
	if !isHijacker {
		t.Error("Writer does not implement http.Hijacker")
	}
}
