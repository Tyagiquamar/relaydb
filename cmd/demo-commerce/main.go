package main

import (
	"fmt"
	"log"

	"github.com/tyagiquamar/relaydb/internal/config"
)

func main() {
	cfg := config.MustLoad()
	_ = cfg
	fmt.Println("demo-commerce not yet implemented")
	log.Println("This will write demo order data to the source database")
}