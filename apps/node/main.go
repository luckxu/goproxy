package main

import (
	"flag"
	"strconv"
)

var UUID *string
func main() {
	host := flag.String("host", "127.0.0.1", "proxy host 代理服务器地址")
	port := flag.Int("port", 925, "proxy port 代理端口")
	UUID = flag.String("uuid", "idste", "UUID")
	flag.Parse()
	if *port > 40000 || *port <= 0 {
		panic("端口错误，1-40000")
	}
	NewNode(*host, strconv.Itoa(*port))
	select {}
}
