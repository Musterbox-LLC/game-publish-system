// utils/http.go (example)
package utils

import (
	"net/http"
	"time"
)

var HTTPClient = &http.Client{
	Timeout: 300 * time.Second, // 5 minutes for large downloads
}