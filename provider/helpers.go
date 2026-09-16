package provider

import (
	"errors"
	"fmt"
	"net/http"

	lock "github.com/use-lock/client-go"
)

func apiError(operation string, status int) error {
	return fmt.Errorf("lock: %s returned HTTP %d or an unexpected response body", operation, status)
}

func requestError(operation string, err error) error {
	var response *lock.APIError
	if errors.As(err, &response) {
		return apiError(operation, response.StatusCode)
	}
	return err
}

func isNotFound(err error) bool {
	var response *lock.APIError
	return errors.As(err, &response) && response.StatusCode == http.StatusNotFound
}
