package main

import (
	"fmt"
	"net"
	"os"
	"os/signal"

	"edgaru089.ink/go/regolith/internal/http"
)

func main() {

	listener, err := net.Listen("tcp", ":3128")
	if err != nil {
		panic(err)
	}

	sigint_chan := make(chan os.Signal, 1)
	signal.Notify(sigint_chan, os.Interrupt)
	go func() {
		<-sigint_chan
		listener.Close()
	}()

	s := &http.Server{}
	err = s.Serve(listener)
	if err != nil {
		fmt.Println(err)
	}
}
