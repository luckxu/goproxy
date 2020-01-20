// Copyright 2019 The goproxy Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package proxy

import (
	"net"
	"time"
)

type callback func(p *Proxy)

const (
	CTRL_CMD_EXIT        = 0
	CTRL_CMD_FORCE_EXIT = 1
	CTRL_CMD_DATA       = 2
	CTRL_CMD_TICK       = 3
)

//最多支持32条命令
const (
	PROXY_CMD_DATA          = 0
	PROXY_CMD_PAUSE         = 1
	PROXY_CMD_RUN           = 2
	PROXY_CMD_NEW_CONNECT   = 3
	PROXY_CMD_CLOSE_CONNECT = 4
	PROXY_CMD_NEW_LISTEN    = 5
	PROXY_CMD_KEEPALIVE     = 6
)

const (
	PROXY_FLAG_HEAD_DECRYPTED = 1
)

const (
	TICK_MS = time.Second / 10
)

type Address struct {
	Domain string
	Addr   string
}

type Listener struct {
	Listen  Address
	Forward Address
	active  bool
	//监听句柄
	l net.Listener
}
