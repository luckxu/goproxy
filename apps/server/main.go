// Copyright 2019 The goproxy Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

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

var UUID *string
func main() {
	var listeners arg_list
	var peerListeners arg_list
	host := flag.String("host", "0.0.0.0", "listen host ip代理服务监听地址")
	port := flag.Int("port", 925, "listen port代理服务监听端口")
	UUID  = flag.String("uuid", "idste", "UUID")
	flag.Var(&listeners, "listener", "listen&forward address list代理端监听转发地址，可多次传入该参数")
	flag.Var(&peerListeners, "peer_listener", "peer listen&forward address list内网代理转发地址，可多次传入该参数")
	flag.Parse()
	if *port > 40000 || *port <= 0 {
		panic("端口错误，1-40000")
	}
	NewServer(*host, strconv.Itoa(*port), listeners, peerListeners)
	select {}
}
