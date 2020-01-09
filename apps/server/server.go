// Copyright 2019 The goproxy Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"crypto/aes"
	"crypto/md5"
	"crypto/rand"
	"fmt"
	"github.com/idste/goproxy/proxy"
	"io"
	"net"
	"sync"
	"time"
)

type Server struct {
	active     bool
	id         uint32
	listenAddr proxy.Address
	mutex      sync.RWMutex
	l          net.Listener
	proxys     map[uint32]*proxy.Proxy
	bp         *proxy.BufferPool
}

func serverProxyExit(p *proxy.Proxy) {
	s := p.Ctx.(*Server)
	s.mutex.Lock()
	delete(s.proxys, p.ID)
	s.mutex.Unlock()
}

//自定义登录过程，完成连接认证和aes128密钥下发
func (s *Server) login(c net.Conn) (p *proxy.Proxy, cli *client, ok bool) {
	data := make([]byte, 4096)
	for {
		//read flag "  v1"
		_ = c.SetReadDeadline(time.Now().Add(time.Second * 5))
		if n, err := c.Read(data); err != nil || n < 4 {
			break
		}
		if !bytes.Equal(data[0:4], []byte("  v1")) {
			break
		}
		if n, err := c.Write([]byte("hello")); err != nil || n < 4 {
			break
		}
		//read uuid
		_ = c.SetReadDeadline(time.Now().Add(time.Second * 5))
		n, err := c.Read(data)
		if err != nil || n <= 0 {
			break
		}

		cli, ok := clients[string(data[0:n])]
		if !ok {
			break
		}
		//generate 16bytes random string
		var tmpstr [16]byte
		if _, err := io.ReadFull(rand.Reader, tmpstr[0:]); err != nil {
			break
		}

		if n, err := c.Write(tmpstr[0:]); err != nil || n < 16 {
			break
		}

		copy(data[0:16], tmpstr[0:16])
		copy(data[16:], cli.password)
		size := 16 + len(cli.password)
		md5sum := md5.Sum(data[:size])
		blk, err := aes.NewCipher(md5sum[0:16])
		if err != nil {
			break
		}
		//read aes encrypted data, then decrypt it and compare with tmpstr
		_ = c.SetReadDeadline(time.Now().Add(time.Second * 5))
		if n, err := c.Read(data); err != nil || n != 16 {
			break
		}
		blk.Decrypt(data[0:16], data[0:16])
		if !bytes.Equal(data[0:16], tmpstr[0:16]) {
			break
		}
		s.mutex.Lock()
		for {
			if _, ok := s.proxys[s.id]; ok == true {
				s.id++
			} else {
				break
			}
		}
		p := proxy.NewProxy(s.id, c, s, blk, s.bp, serverProxyExit)
		s.proxys[s.id] = p
		s.mutex.Unlock()
		return p, cli, true
	}
	return nil, nil, false
}

func (s *Server) handle(c net.Conn) {
	var p *proxy.Proxy = nil
	p, cli, ok := s.login(c)
	if ok == false {
		_ = c.Close()
		fmt.Printf("登录失败\n")
		return
	}
	if len(cli.list) == 0 {
		fmt.Printf("增加listen参数可设置本端监听地址， 示例: -listener '{\"Listen\":{\"Domain\":\"tcp\",\"Addr\":\"127.0.0.1:1511\"},\"Forward\":{\"Domain\":\"tcp\", \"Addr\":\"127.0.0.1:80\"}}'\n")
		fmt.Printf("增加peer_listen参数可添加对端监听地址， 示例: -peer_listener '{\"Listen\":{\"Domain\":\"tcp\",\"Addr\":\"127.0.0.1:1511\"},\"Forward\":{\"Domain\":\"tcp\", \"Addr\":\"127.0.0.1:80\"}}'\n")
		_ = c.Close()
		return
	}
	go p.Handle()
	for _, v := range cli.list {
		if v.kind == LISTEN {
			p.NewListener([]byte(v.addr))
		} else {
			p.NewPeerListener([]byte(v.addr))
		}
	}
}

func (s *Server) newListen() {
	for {
		for {
			l, err := net.Listen("tcp", s.listenAddr.Addr)
			if err == nil {
				s.l = l
				break
			}
			fmt.Printf("监听失败(%s)，稍后重试\n", s.listenAddr.Addr)
			time.Sleep(5 * time.Second)
		}
		for {
			c, err := s.l.Accept()
			if err != nil {
				fmt.Println("accept tcp connection failed:", err)
				break
			}
			go s.handle(c)
		}
		if !s.active {
			break
		}
	}
}

func NewServer(addr string) {
	s := &Server{active: true, listenAddr: proxy.Address{Domain: "tcp", Addr: addr}}
	s.proxys = make(map[uint32]*proxy.Proxy)
	s.bp = proxy.NewBufferPool(10240)
	go s.newListen()
}
