//go:build !tpod_repair_testroot

package main

import (
	"net/http"
	"time"
)

func repairHTTPClient() *http.Client {
	return &http.Client{Timeout: 30 * time.Second}
}
