package iam

import (
	"encoding/json"
	"net/http"
	"regexp"
)

const MaxBodySize = 1 << 20 // 1 MB

var uuidRegex = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// ValidUUID checks whether s is a valid lowercase UUID v4 format.
func ValidUUID(s string) bool {
	return uuidRegex.MatchString(s)
}

// DecodeJSON limits the request body to MaxBodySize, decodes JSON into v,
// and writes an error response on failure. Returns true on success.
func DecodeJSON(w http.ResponseWriter, r *http.Request, v interface{}) bool {
	r.Body = http.MaxBytesReader(w, r.Body, MaxBodySize)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		if err.Error() == "http: request body too large" {
			WriteError(w, http.StatusRequestEntityTooLarge, "invalid_request", "request body too large")
		} else {
			WriteError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		}
		return false
	}
	return true
}
