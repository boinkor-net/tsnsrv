package main

import (
	"context"
	"errors"
	"log"
	"os"

	"github.com/boinkor-net/tsnsrv"
	"github.com/peterbourgon/ff/v3/ffcli"
)

func main() {
	s, cmd, err := tsnsrv.TailnetSrvFromArgs(os.Args)
	if err != nil {
		log.Fatalf("Invalid CLI usage. Errors:\n%v\n\n%v", errors.Unwrap(err), ffcli.DefaultUsageFunc(cmd))
	}
	if err := s.Run(context.Background()); err != nil {
		log.Fatal(err)
	}
}
