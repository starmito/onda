package api

import (
	"os"
	"strings"
)

var Version string = "unknown"

func init() {
	data, err := os.ReadFile("VERSION")
	if err != nil {
		data, err = os.ReadFile("/VERSION")
		if err != nil {
			return
		}
	}
	Version = strings.TrimSpace(string(data))
}
