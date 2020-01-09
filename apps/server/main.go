// Copyright 2019 The goproxy Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"
	"github.com/bitly/go-simplejson"
	"io/ioutil"
	"strconv"
)

const (
	LISTEN = iota
	PEER_LISTEN
)

type listen struct {
	kind int
	addr string
}

type client struct {
	password string
	list map[int]*listen
}

type arg_list[] string
var  listenType = [2]string{LISTEN:"listen", PEER_LISTEN: "peerListen"}

var clients map[string]*client

func init() {
	clients = make(map[string]*client)
}

func (i *arg_list) String() string {
	return ""
}

func (i *arg_list) Set(value string) error {
	*i = append(*i, value)
	return nil
}

func loadConfig(path string) {
	body, err := ioutil.ReadFile(path)
	if err != nil {
		fmt.Printf("read config file(%s) error:%s\n", path, err.Error())
		return
	}

	js, err := simplejson.NewJson(body);
	if err != nil {
		fmt.Printf("create new json object failed:%s\n", err.Error())
		return
	}

	jclients, ok := js.CheckGet("clients")
	if !ok {
		fmt.Printf("can not get `clients` from config file(%s)\n", path)
		return;
	}

	i := 0
	id := 0
	for {
		jclient := jclients.GetIndex(i)
		i++
		uuid, err := jclient.Get("uuid").String()
		if err != nil {
			break
		}
		password, err := jclient.Get("password").String()
		if err != nil {
			break
		}
		cli := &client{password:password}
		cli.list = make(map[int]*listen)
		for kind, v := range listenType {
			if jl, ok := jclient.CheckGet(v); ok {
				k := 0
				for {
					addr := jl.GetIndex(k)
					k++
					if t, ok := addr.CheckGet("Listen"); !ok {
						break
					} else {
						if _, ok := t.CheckGet("Domain"); !ok {
							break
						}
						if _, ok := t.CheckGet("Addr"); !ok {
							break
						}
					}
					if t, ok := addr.CheckGet("Forward"); !ok {
						break
					} else {
						if _, ok := t.CheckGet("Domain"); !ok {
							break
						}
						if _, ok := t.CheckGet("Addr"); !ok {
							break
						}
					}
					lsn := &listen{kind: kind}
					ctx, err := addr.Encode()
					if err != nil {
						continue
					}
					lsn.addr = string(ctx)
					cli.list[id] = lsn
					id++;
				}
			}
		}
		if len(cli.list) > 0 {
			clients[uuid] = cli
		}
	}
}
func main() {
	var listeners arg_list
	var peerListeners arg_list
	host := flag.String("host", "0.0.0.0", "listen host ip代理服务监听地址")
	port := flag.Int("port", 925, "listen port代理服务监听端口")
	uuid := flag.String("uuid", "idste", "UUID")
	password := flag.String("password", "1e4d4e53556a1bb5f6adf4753e7956cb", "password")
	configPath := flag.String("config_path", "/etc/goproxy.conf", "config file")
	flag.Var(&listeners, "listener", "listen&forward address list代理端监听转发地址，可多次传入该参数")
	flag.Var(&peerListeners, "peer_listener", "peer listen&forward address list内网代理转发地址，可多次传入该参数")
	flag.Parse()
	if *port > 40000 || *port <= 0 {
		panic("端口错误，1-40000")
	}
	loadConfig(*configPath)
	if _, ok := clients[*uuid]; !ok {
		cli := &client{password:*password}
		cli.list = make(map[int]*listen)
		i := 0
		for _, v := range listeners {
			lsn := &listen{kind:LISTEN, addr:string(v)}
			cli.list[i] = lsn
			i++
		}
		for _, v := range peerListeners {
			lsn := &listen{kind:PEER_LISTEN, addr:string(v)}
			cli.list[i] = lsn
			i++
		}
		if len(cli.list) > 0 {
			clients[*uuid] = cli
		}
	}
	for k, v := range clients {
		fmt.Printf("uuid:%s:\n", k)
		for i, v1 := range v.list {
			fmt.Printf("  id:%d password:%s, kind:%s addr:%s\n", i, v.password, listenType[v1.kind], v1.addr)
		}
	}
	NewServer(*host + ":" +strconv.Itoa(*port))
	select {}
}
