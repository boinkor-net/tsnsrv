package main

import (
	"errors"
	"log"
	"os"
	"os/signal"

	"github.com/boinkor-net/tsnsrv"
	"github.com/peterbourgon/ff/v3/ffcli"
)

func main() {
	_, cmd, err := tsnsrv.TailnetSrvFromArgs(os.Args)
	if err != nil {
		log.Fatalf("Invalid CLI usage. Errors:\n%v\n\n%v", errors.Unwrap(err), ffcli.DefaultUsageFunc(cmd))
	}
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt)
	<-done
}
