// Command curing-outputd owns the relay lines and turns them off when the control
// process stops renewing its lease. See docs/adr/0207-output-lease.md.
package main

import (
	"fmt"
	"os"

	"github.com/johnjezl/curing-cabinet-controller/internal/drivers"
)

func main() {
	if err := drivers.RunOutputGuard(drivers.DefaultConfig()); err != nil {
		fmt.Printf("<2>curing-outputd: %v\n", err)
		os.Exit(1)
	}
}
