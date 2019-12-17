package main

import (
	"flag"
	"strconv"
)

func main() {
	host := flag.String("host", "127.0.0.1", "listen host ip")
	port := flag.Int("port", 925, "listen port")
	flag.Parse()
	if *port > 40000 || *port <= 0 {
		panic("port can be 1 - 40000")
	}
	NewNode(*host, strconv.Itoa(*port))
	select {}
}
