//go:build !demoinject

package main

import (
	"context"
	"flag"

	"github.com/johnjezl/curing-cabinet-controller/internal/api"
	"github.com/johnjezl/curing-cabinet-controller/internal/ui"
)

// Without the demoinject build tag there is no injection path at all: no flag,
// no reader, no code.
func addInjectFlag(*flag.FlagSet) *string { return nil }

func startInject(context.Context, *string, chan<- ui.TouchEvent) {}

func addAutotapFlag(*flag.FlagSet) *string { return nil }

type realCoreFlags struct{}

func addRealCoreFlags(*flag.FlagSet) *realCoreFlags { return nil }

func startRealCore(context.Context, *realCoreFlags) api.Controller { return nil }

func startAutotap(context.Context, *string, chan<- ui.TouchEvent) {}
