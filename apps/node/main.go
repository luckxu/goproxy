// Copyright 2019 The goproxy Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"strconv"
)

var UUID *string
func main() {
	host := flag.String("host", "127.0.0.1", "proxy host 代理服务器地址")
	port := flag.Int("port", 925, "proxy port 代理端口")
	password := flag.String("password", "1e4d4e53556a1bb5f6adf4753e7956cb", "password")
	UUID = flag.String("uuid", "idste", "UUID")
	flag.Parse()
	if *port > 40000 || *port <= 0 {
		panic("端口错误，1-40000")
	}
	NewNode(*host + ":" + strconv.Itoa(*port), *UUID, *password)
	select {}
}
