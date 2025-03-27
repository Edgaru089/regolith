package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"edgaru089.ink/go/regolith/internal/conf"
	"edgaru089.ink/go/regolith/internal/http"
	"edgaru089.ink/go/regolith/internal/perm"
)

func main() {
	var s *http.Server

	{
		perm_buf, err := os.ReadFile("perm.json")
		if err != nil {
			panic(err)
		}
		perm_json := make(map[string]perm.Config)
		err = json.Unmarshal(perm_buf, &perm_json)
		if err != nil {
			panic(err)
		}
		s = &http.Server{
			Perm: perm.New(perm_json),
		}
	}

	var conf conf.Config
	{
		conf_buf, err := os.ReadFile("config.json")
		if err != nil {
			panic(err)
		}
		err = json.Unmarshal(conf_buf, &conf)
		if err != nil {
			panic(err)
		}
	}

	listener, err := net.Listen(conf.ListenType, conf.ListenAddress)
	if err != nil {
		panic(err)
	}
	log.Printf("listeneing on [%s], type %s", conf.ListenAddress, conf.ListenType)

	sigint_chan := make(chan os.Signal, 1)
	signal.Notify(sigint_chan, os.Interrupt)
	go func() {
		<-sigint_chan
		log.Printf("SIGINT received, quitting")
		listener.Close()
	}()

	sighup_chan := make(chan os.Signal, 1)
	signal.Notify(sighup_chan, syscall.SIGHUP)
	go func() {
		for {
			<-sighup_chan
			log.Printf("SIGHUP received, reloading permissions")
			perm_buf, err := os.ReadFile("perm.json")
			if err != nil {
				log.Printf("skipping reload: error opening perm.json: %e", err)
				continue
			}
			perm_json := make(map[string]perm.Config)
			err = json.Unmarshal(perm_buf, &perm_json)
			if err != nil {
				log.Printf("skipping reload: error unmarshaling perm.json: %e", err)
				continue
			}
			s.Perm.Load(perm_json)
		}
	}()

	err = s.Serve(listener)
	if err != nil {
		fmt.Println(err)
	}
}
