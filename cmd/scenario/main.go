// Command scenario runs the plant simulation against the controller and writes
// raw per-sample CSVs, event logs and machine-computed metrics.
//
//	scenario list
//	scenario run -name door_2min_13c -plant base -out DIR
//	scenario suite -out evidence/a -sweep 36 -j 8
//	scenario cure -out DIR -recipe ferment-dry -plant base -days 90 -outages 8
//	scenario restart -out DIR -cuts 60 -plant base
//	scenario cutmatrix -out DIR -ops 300
//	scenario crash -out DIR -n 30
//	scenario verify -dir evidence/a
//
// Plants: base, a corner name (c01-...), or sNN from the sweep.
package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: scenario list|run|suite|cure|restart|cutmatrix|crash|crashchild|verify ...")
		os.Exit(2)
	}
	var err error
	args := os.Args[2:]
	switch os.Args[1] {
	case "list":
		cmdList()
	case "run":
		err = cmdRun(args)
	case "suite":
		err = cmdSuite(args)
	case "cure":
		err = cmdCure(args)
	case "restart":
		err = cmdRestart(args)
	case "cutmatrix":
		err = cmdCutMatrix(args)
	case "crash":
		err = cmdCrash(args)
	case "crashchild":
		err = cmdCrashChild(args)
	case "verify":
		err = cmdVerify(args)
	case "barsmoved":
		err = cmdBarsMoved(args)
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func newFlags(name string) *flag.FlagSet { return flag.NewFlagSet(name, flag.ExitOnError) }
