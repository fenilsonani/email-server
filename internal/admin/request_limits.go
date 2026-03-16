package admin

import (
	"net/http"
	"strings"
)

const (
	maxAdminFormBody      int64 = 1 << 20
	maxAdminMultipartBody int64 = 500 << 20
)

func parseFormWithLimit(w http.ResponseWriter, r *http.Request, maxBytes int64) error {
	if r.Body != nil {
		r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	}
	return r.ParseForm()
}

func parseMultipartFormWithLimit(w http.ResponseWriter, r *http.Request, maxMemory, maxBytes int64) error {
	if r.Body != nil {
		r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	}
	return r.ParseMultipartForm(maxMemory)
}

func requestBodyTooLarge(err error) bool {
	return err != nil && strings.Contains(err.Error(), "http: request body too large")
}

func formErrorStatus(err error) int {
	if requestBodyTooLarge(err) {
		return http.StatusRequestEntityTooLarge
	}
	return http.StatusBadRequest
}
