package main

import (
	"fmt"
	"log"

	"github.com/tyagiquamar/relaydb/internal/config"
)

func main() {
	cfg := config.MustLoad()
	_ = cfg
	fmt.Println("loadgen not yet implemented")
	log.Println("This will generate load for benchmarking")
}