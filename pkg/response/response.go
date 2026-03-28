package response

import (
	"encoding/json"
	"net/http"
)

type ProcessResult struct {
	Success       bool              `json:"success"`
	Filename      string            `json:"filename"`
	OriginalSize  int64             `json:"originalSize"`
	ProcessedSize int64             `json:"processedSize"`
	Reduction     string            `json:"reduction"`
	Width         int               `json:"width"`
	Height        int               `json:"height"`
	Format        string            `json:"format"`
	Duration      string            `json:"duration"`
	ContentType   string            `json:"contentType"`
}

type ErrorResponse struct {
	Error   string `json:"error"`
	Code    string `json:"code"`
	Details string `json:"details,omitempty"`
}

func JSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func Error(w http.ResponseWriter, status int, code, message string) {
	JSON(w, status, ErrorResponse{Error: message, Code: code})
}
