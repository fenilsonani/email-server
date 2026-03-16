package userportal

import (
	"net/http"
	"strings"
)

const maxUserPortalFormBody int64 = 1 << 20

func parseFormWithLimit(w http.ResponseWriter, r *http.Request, maxBytes int64) error {
	if r.Body != nil {
		r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	}
	return r.ParseForm()
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
