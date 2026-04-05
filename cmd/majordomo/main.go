package main

import (
	"os"

	"github.com/superset-studio/majordomo-gateway/gateway"
)

func main() {
	gateway.RunCLI(os.Args)
}
