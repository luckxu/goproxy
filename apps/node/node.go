// Copyright 2019 The goproxy Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"crypto/aes"
	"crypto/md5"
	"fmt"
	"github.com/idste/goproxy/proxy"
	"net"
	"strings"
	"time"
)

type Node struct {
	active bool
	c      net.Conn
	proxy  *proxy.Proxy
	bp     *proxy.BufferPool
	addr   proxy.Address
	uuid	 string
	password string
}

func nodeProxyExit(p *proxy.Proxy) {
	n := p.Ctx.(*Node)
	n.proxy = nil
	if n.active {
		go n.newConnect()
	}
}

//自定义登录过程，完成连接认证和aes128密钥获取
func (n *Node) login(c net.Conn) (p *proxy.Proxy, ok bool) {
	data := make([]byte, 4096)
	for {
		//发送标识"  v1"
		if _, err := c.Write([]byte("  v1")); err != nil {
			break
		}
		//读取hello
		if n, err := c.Read(data); err != nil || n != 5 || bytes.Equal(data, []byte("hello")){
			break
		}

		//发送uuid
		if _, err := c.Write([]byte(n.uuid)); err != nil {
			break
		}

		//读取16字节随机数
		if n, err := c.Read(data); err != nil || n != 16 {
			break
		}
		copy(data[16:], n.password)
		size := 16 + len(n.password)
		md5sum := md5.Sum(data[:size])
		blk, err := aes.NewCipher(md5sum[0:16])
		if err != nil {
			break
		}
		blk.Encrypt(data[0:16], data[0:16])
		//发送aes encrypt data[0:16]
		if _, err := c.Write(data[0:16]); err != nil {
			break
		}
		p := proxy.NewProxy(0, c, n, blk, n.bp, nodeProxyExit)
		n.proxy = p
		return p, true
	}
	return nil, false
}

func (n *Node) newConnect() {
	for {
		s := strings.Split(n.addr.Addr, ":")
		if len(s) != 2 {
			fmt.Printf("地址错误(%s)，稍后重试\n", n.addr.Addr)
			break
		}
		ips, err := net.LookupHost(s[0])
		if err != nil {
			fmt.Printf("未知主机(%s)，稍后重试\n", n.addr.Addr)
		}
		c, err := net.Dial(n.addr.Domain, ips[0]+":"+s[1])
		if err == nil {
			n.c = c
			//首先完成登录
			if p, ok := n.login(c); ok == true {
				fmt.Printf("连接成功\n")
				go p.Handle()
				break
			}
		}
		fmt.Printf("连接失败(%s)，稍后重试\n")
		time.Sleep(1 * time.Second)
	}
}

func NewNode(addr string, uuid string, password string) {
	n := &Node{active: true, uuid:uuid, password: password, addr: proxy.Address{Domain: "tcp", Addr: addr}}
	n.bp = proxy.NewBufferPool(10240)
	go n.newConnect()
}
