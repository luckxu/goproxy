// Copyright 2019 The goproxy Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package proxy

import (
	"fmt"
	"net"
	"sync"
	"time"
)

type client struct {
	//是否是监听子连接
	subtype bool
	//是否暂停接收，用于流控
	//当待发送缓存数量多于pauseTh需要发送PROXY_CMD_PAUSE消息通知暂时数据发送
	//当待发送缓存数量少于startTh需要发送PROXY_CMD_RUN消息通知恢复数据发送
	pause bool
	//是否已发送暂停命令
	sendPause bool
	//id 在同一proxy对象内，每个client都有唯一ID
	id uint32
	//连接句柄
	c net.Conn
	//proxy对象
	proxy *Proxy
	//控制通道
	ctrlChan chan byte
	//退出命令控制通道
	exitChan chan byte
	//发送缓存列表
	sendBuffers *bufferHeader
	wg          sync.WaitGroup
}

//NewProxy创建新的代理对象
//@subtype 是否是监听子连接
func NewClient(id uint32, c net.Conn, proxy *Proxy, subtype bool) *client {
	cli := &client{id: id, c: c, proxy: proxy, subtype: subtype}
	cli.ctrlChan = make(chan byte, 256)
	cli.exitChan = make(chan byte, 16)
	//发送缓存数大于16即会向对端发送暂停命令
	cli.sendBuffers = newBufferHeader(32, proxy.bp)
	return cli
}

//读取数据go程，除超时外任何错误都关闭连接
func (cli *client) read() {
	cli.wg.Add(1)
	var b *buffer = nil
	for {
		if b == nil {
			b = cli.proxy.bp.get()
			if b == nil {
				fmt.Println("allocate new buffer failed, exit")
				goto err
			}
		}
		//主动限速
		if cli.pause {
			time.Sleep(time.Second / 20)
		}
		//超时定时器，用于产生CTRL_CMD_TICK，定时清理空闲缓存
		_ = cli.c.SetReadDeadline(time.Now().Add(TICK))
		//前8字节为头部，预留
		n, err := cli.c.Read(b.data[8:])
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() == true {
				continue
			}
			goto err
		}
		b.size = n + 8
		//发送数据，如果主连接发送不及时会在函数内阻塞
		cli.proxy.clientSendCommand(cli, PROXY_CMD_DATA, b, nil)
		b = nil
	}
err:
	//退出
	cli.wg.Done()
	cli.ctrlChan <- CTRL_CMD_EXIT
}

//写数据
func (cli *client) write() {
	go cli.read()
	//空闲缓存清理定时器
	forceExit := false
	for {
		select {
		case cmd := <-cli.exitChan:
			//强制关闭连接且不发送关闭消息
			if cmd == CTRL_CMD_FORCE_EXIT {
				forceExit = true
				goto err
			}
		case cmd := <-cli.ctrlChan:
			switch cmd {
			case CTRL_CMD_EXIT:
				goto err
			case CTRL_CMD_DATA:
				b := cli.sendBuffers.pop()
				if b == nil {
					break
				}
				//前8字节为头部数据，忽略
				offset := 8
				for {
					cnt, err := cli.c.Write(b.data[offset:b.size])
					if err != nil {
						goto err
					}
					if cnt+offset == b.size {
						break
					}
					offset += cnt
					continue
				}
				if cli.sendPause == false && cli.sendBuffers.almostFull() {
					//处理数据缓存过多时暂停对端子连接接收
					cli.proxy.clientSendCommand(cli, PROXY_CMD_PAUSE, nil, nil)
					cli.sendPause = true
				} else if cli.sendPause == true && cli.sendBuffers.almostEmpty() {
					//处理数据缓存过少时恢复对端子连接接收
					cli.proxy.clientSendCommand(cli, PROXY_CMD_RUN, nil, nil)
					cli.sendPause = false
				}
				//归还至空闲缓存池，如果池中缓存长时间未使用，会在定时器中归还至根缓存池
				cli.proxy.bp.put(b)
			case CTRL_CMD_FORCE_EXIT:
				forceExit = true
				goto err
			}
		}
	}
err:
	cli.c.Close()
	cli.wg.Wait()
	clean:
	for {
		select {
		case <-cli.exitChan:
		case <-cli.ctrlChan:
		default:
			break clean
		}
	}
	if forceExit == false {
		//非强制关闭时发送连接已关闭消息
		cli.proxy.clientSendCommand(cli, PROXY_CMD_CLOSE_CONNECT, nil, nil)
	}
	cli.proxy.clientExit(cli)
}

func (cli *client) handle() {
	cli.write()
}
