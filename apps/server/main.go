package main

import (
	"flag"
	"strconv"
)
type arg_list[] string

func (i *arg_list) String() string {
	return ""
}

func (i *arg_list) Set(value string) error {
	*i = append(*i, value)
	return nil
}
func main() {
	host := flag.String("host", "127.0.0.1", "listen host ip")
	port := flag.Int("port", 925, "listen port")
	var listeners arg_list
	var peerListeners arg_list
	flag.Var(&listeners, "listener", "listen&forward address list")
	flag.Var(&peerListeners, "peer_listener", "peer listen&forward address list")
	flag.Parse()
	if *port > 40000 || *port <= 0 {
		panic("port can be 1 - 40000")
	}
	NewServer(*host, strconv.Itoa(*port), listeners, peerListeners)
	select {}
}
