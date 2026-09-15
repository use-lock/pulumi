package provider

import "fmt"

func apiError(operation string, status int) error {
	return fmt.Errorf("lock: %s returned HTTP %d or an unexpected response body", operation, status)
}
