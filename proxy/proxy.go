// Copyright 2019 The goproxy Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package proxy

/*
代理类，服务端和客户端完全对称，两端都可生成监听地址并接受连接，新连接将获得唯一32位无符号ID，
通过NEW_CONNECT命令将ID和转发地址发送至对端，由对端连接至最终目的地，双方通过唯一ID识别转发的数据
*/
import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/json"
	"fmt"
	reuse "github.com/libp2p/go-reuseport"
	"net"
	"sync"
	"time"
)

//代理主机可以产生子连接和监听子连接
//对端产生监听子连接后通过NEW_CONNECT命令通知本端并产生本端子连接
//本端产生监听子连接后通过相同方式在对端产生对端子连接
//两端的子连接通过ID关联
type Proxy struct {
	//监听子连接ID计数器， 子连接ID由对端指定
	idx uint32
	//当前代理的ID
	ID  uint32
	keepaliveAt int64
	//主连接
	c net.Conn
	//缓存池
	bp *BufferPool
	//发送缓存
	sendChan chan *buffer
	//紧急消息
	emergencyChan chan *buffer
	//控制通道，发送CTRL_CMD_XX命令
	ctrlChan chan byte
	//空闲缓存
	freeBuffers *bufferHeader
	//加密aes128密钥
	aesBlock cipher.Block
	//保护锁
	mutex sync.RWMutex
	//所有子连接go程计数、子连接、监听子连接列表
	wg          sync.WaitGroup
	clients     map[uint32]*client
	subClients  map[uint32]*client
	//监听索引和列表
	listenerIdx int
	listeners   map[int]*Listener
	//退出参数和回调函数
	Ctx         interface{}
	exitCB      callback
}

//NewProxy创建新的代理对象
//在调用本函数前，需要完成服务端和客户端连接并完成认证和aes128密钥协商
func NewProxy(id uint32, c net.Conn, ctx interface{}, aesBlock cipher.Block, bp *BufferPool, exit callback) *Proxy {
	if bp == nil || aesBlock == nil || c == nil {
		panic("buffer pool can not be nil")
	}
	p := &Proxy{ID: id, idx: 1, aesBlock: aesBlock, c: c, bp: bp, exitCB: exit, Ctx: ctx}
	p.keepaliveAt = time.Now().Unix()
	p.sendChan = make(chan *buffer, 256)
	p.emergencyChan = make(chan *buffer, 16)
	p.ctrlChan = make(chan byte, 64)
	p.freeBuffers = p.newBufferHeader(128)
	p.clients = make(map[uint32]*client)
	p.subClients = make(map[uint32]*client)
	p.listeners = make(map[int]*Listener)
	return p
}

//创建缓存头bufferHeader
func (p *Proxy) newBufferHeader(holdcnt int) *bufferHeader {
	return &bufferHeader{pool: p.bp, holdcnt:holdcnt}
}

//尝试从空闲缓存或缓存池中分配缓存
func (p *Proxy) getBuffer() *buffer {
	b := p.freeBuffers.pop()
	if b == nil {
		b = p.bp.get()
		if b == nil {
			fmt.Printf("alloc new buffer failed.\n")
		}
	}
	return b
}

//aes128加密缓存，分两次加密，先加密头部，再加密数据区
//@b 缓存
func (p *Proxy) encryptBuffer(b *buffer) {
	padding := byte(0)
	//计算补零数
	if b.size%aes.BlockSize != 0 {
		padding = byte(aes.BlockSize - b.size%aes.BlockSize)
	}
	//byte1高4位保留，低4位表示补齐字符数
	b.data[1] &= 0xf0
	b.data[1] += padding
	b.size += int(padding)
	b.data[2] = byte(b.size)
	b.data[3] = byte(b.size >> 8)
	p.aesBlock.Encrypt(b.data[0:aes.BlockSize], b.data[0:aes.BlockSize])
	if b.size > aes.BlockSize {
		p.aesBlock.Encrypt(b.data[aes.BlockSize:b.size], b.data[aes.BlockSize:b.size])
	}
}

//子连接退出回调
//@cli 子连接
func (p *Proxy) clientExit(cli *client) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	p.bp.appendList(cli.sendBuffers)
	p.bp.appendList(cli.freeBuffers)
	if cli.subtype {
		delete(p.subClients, cli.id)
	} else {
		delete(p.clients, cli.id)
	}
	p.wg.Done()
}

//接受子连接，在成功接受后向分配ID并向对端发送NEW_CONNECT命令
//@la 监听句柄，用于获取转发地址
//@c 接受的子连接
func (p *Proxy) accept(la *Listener, c net.Conn) {
	body, err := json.Marshal(la.Forward)
	if err != nil {
		fmt.Printf("json marshal error, addr:%+v.\n", la.Forward)
		return
	}
	p.mutex.Lock()
	for {
		p.idx++
		if p.idx == 0 {
			continue
		}
		if _, ok := p.subClients[p.idx]; ok == false {
			break
		}
	}
	cli := NewClient(p.idx, c, p, true)
	p.subClients[p.idx] = cli
	p.mutex.Unlock()
	p.wg.Add(1)
	go cli.handle()
	//发送新连接命令
	p.sendCommand(cli.subtype, cli.id, PROXY_CMD_NEW_CONNECT, nil, body)
}

//通知对端在新的地址上监听连接
//@msg 监听地址和转发地址json字串
//@msg 示例:[]byte("{\"Listen\":{\"Domain\":\"tcp\",\"Addr\":\"127.0.0.1:1513\"},\"Forward\":{\"Domain\":\"tcp\", \"Addr\":\"127.0.0.1:1022\"}}")
func (p *Proxy) NewPeerListener(msg []byte) {
	p.sendCommand(true, 0, PROXY_CMD_NEW_LISTEN, nil, msg)
}

//创建新的监听地址
//@msg 监听地址和转发地址json字串
//@msg 示例:[]byte("{\"Listen\":{\"Domain\":\"tcp\",\"Addr\":\"127.0.0.1:1513\"},\"Forward\":{\"Domain\":\"tcp\", \"Addr\":\"127.0.0.1:1022\"}}")
func (p *Proxy) NewListener(msg []byte) {
	var lsn Listener
	if err := json.Unmarshal(msg, &lsn); err != nil {
		fmt.Printf("json unmarshal error:%s.\n", err)
		return
	}
	id := -1
	go func() {
		for {
			for {
				l, err := reuse.Listen(lsn.Listen.Domain, lsn.Listen.Addr)
				if err != nil || l == nil {
					fmt.Printf("tcp listen (%s/%s)failed:%s.\n", lsn.Listen.Domain, lsn.Listen.Addr, err)
					time.Sleep(time.Second * 1)
					continue
				}
				lsn.active = true
				lsn.l = l
				p.mutex.Lock()
				for {
					p.listenerIdx++
					if _, ok := p.listeners[p.listenerIdx]; ok == true {
						continue
					}
					break
				}
				id = p.listenerIdx
				//删除已有句柄
				if _, ok := p.listeners[id]; ok != true {
					delete(p.listeners, id)
				}
				p.listeners[id] = &lsn
				p.mutex.Unlock()
				break
			}
			for {
				c, err := lsn.l.Accept()
				if err != nil {
					if (lsn.active) {
						fmt.Printf("accept tcp connection failed, error:%s\n", err.Error())
					}
					break
				}
				if c != nil {
					go p.accept(&lsn, c)
				}
			}
			if !lsn.active {
				p.mutex.Lock()
				delete(p.listeners, id)
				p.mutex.Unlock()
				break
			}
		}
	}()
}

//创建子连接，对端的监听地址上产生新连接时通过NET_CONNECT命令将待连接本地址址通知本端
//@id对端分配的连接ID
//@msg连接地址json字串
func (p *Proxy) newConnection(id uint32, msg []byte) (bufferUsed bool) {
	for {
		var addr Address
		if err := json.Unmarshal(msg, &addr); err != nil {
			fmt.Printf("json unmarshal error:%s.\n", err)
			break
		}
		n, err := net.Dial(addr.Domain, addr.Addr)
		if err != nil {
			fmt.Printf("连接到%s %s失败, error:%s.\n", addr.Domain, addr.Addr, err.Error())
			break
		}
		cli := NewClient(id, n, p, false)
		p.mutex.Lock()
		if client, ok := p.clients[id]; ok == true {
			client.ctrlChan <- CTRL_CMD_EXIT
			delete(p.clients, id)
		}
		p.clients[id] = cli
		p.mutex.Unlock()
		p.wg.Add(1)
		go cli.handle()
		return false
	}
	fmt.Printf("创建子连接失败, id:%d", id)
	//发送命令关闭对端监听子连接(本端非监听子连接)
	p.sendCommand(false, id, PROXY_CMD_CLOSE_CONNECT, nil, nil)
	return true
}

//数据处理器，首先处理主连接自有命令
//@b 读入的缓存
//return bufferUsed缓存是否已使用，供调用函数判断是否需要释放缓存
func (p *Proxy) readProc(b *buffer) (bufferUsed bool) {
	bufferUsed = false
	cmd := b.data[0] & 0x0f
	//主连接自有命令,ID无效
	if cmd == PROXY_CMD_NEW_LISTEN {
		p.NewListener(b.data[8:b.size])
		return
	}
	if cmd == PROXY_CMD_KEEPALIVE {
		p.keepaliveAt = time.Now().Unix()
		fmt.Printf("keepalive, id:%d, at:%s\n", p.ID, time.Now().String())
		return
	}
	ok := false
	cli := (*client)(nil)
	id := uint32(b.data[4])
	id += uint32(b.data[5]) << 8
	id += uint32(b.data[6]) << 16
	id += uint32(b.data[7]) << 24
	if cmd == PROXY_CMD_NEW_CONNECT {
		bufferUsed = p.newConnection(id, b.data[8:b.size])
		return
	}
	//子连接命令，通过ID查找对应的连接句柄
	subtype := (b.data[0] & 0x80) != 0
	p.mutex.RLock()
	if subtype {
		//对端监听子连接对应本端子连接
		cli, ok = p.clients[id]
	} else {
		//对端子连接对应本端监听子连接
		cli, ok = p.subClients[id]
	}
	p.mutex.RUnlock()
	//子连接命令或数据
	if ok == false {
		if cmd != PROXY_CMD_CLOSE_CONNECT {
			fmt.Printf("unkown connection, type:%v, id:%d.\n", subtype, id)
			p.sendCommand(!subtype, id, PROXY_CMD_CLOSE_CONNECT, b, nil)
			bufferUsed = true
		}
		return
	}
	switch cmd {
	//子连接暂停接收,将在子连接调用clientSendCommand时阻塞起到收到PROXY_CMD_RUN
	case PROXY_CMD_PAUSE:
		cli.pause = true
		return
	//子连接恢复接收
	case PROXY_CMD_RUN:
		cli.pause = false
		return
	case PROXY_CMD_CLOSE_CONNECT:
		cli.ctrlChan <- CTRL_CMD_EXIT
	case PROXY_CMD_DATA:
		//将缓存发送至子连接
		//使用链表存储待发送数据而非通道
		//向ctrl通道发送消息有新缓存的消息
		cli.sendBuffers.append(b, false)
		cli.ctrlChan <- CTRL_CMD_DATA
		bufferUsed = true
	}
	return
}

//主连接读go程
func (p *Proxy) read() {
	p.wg.Add(1)
	var b *buffer
	size := 0
	flag := 0
	for {
		if b == nil {
			b = p.getBuffer()
			if b == nil {
				goto err
			}
			flag = 0
			size = 0
		}
		//设置读超时并在超时时生成TICK信号用于清理空闲缓存
		_ = p.c.SetReadDeadline(time.Now().Add(TICK_MS))
		n, err := p.c.Read(b.data[size:])
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				p.ctrlChan <- CTRL_CMD_TICK
				continue
			}
			goto err
		}
		size += n
		for {
			//头部未读取完
			if size < aes.BlockSize {
				break
			}
			//头部未解密时需要先解出头部，确认数据大小
			if flag&PROXY_FLAG_HEAD_DECRYPTED == 0 {
				//解密头部
				p.aesBlock.Decrypt(b.data[0:aes.BlockSize], b.data[0:aes.BlockSize])
				b.size = int(b.data[2])
				b.size += int(b.data[3]) << 8
				//数量大于buffer限制的值或数量不少于16字节或size非16字节对齐
				if b.size > cap(b.data) || b.size < aes.BlockSize || (b.size%aes.BlockSize) > 0 {
					goto err
				}
				flag |= PROXY_FLAG_HEAD_DECRYPTED
			}
			//数据未读取完
			if size < b.size {
				break
			}
			//解密剩余数据
			if b.size > aes.BlockSize {
				p.aesBlock.Decrypt(b.data[aes.BlockSize:b.size], b.data[aes.BlockSize:b.size])
			}
			//数据量多于一个包，暂存在newB中
			var newB *buffer = nil
			if b.size < size {
				//有多余数据，循环处理
				newB = p.getBuffer()
				if newB == nil {
					goto err
				}
				copy(newB.data, b.data[b.size:size])
				size -= b.size
			} else {
				size = 0
			}
			flag = 0
			//减去aes加密时补齐的字符
			b.size -= int(b.data[1] & 0x0f)
			bufferUsed := p.readProc(b)
			if !bufferUsed {
				//放入空闲缓存列表
				p.freeBuffers.append(b, true)
			}
			b = nil
			//有多余数据，存放在newB
			if newB != nil {
				b = newB
			} else {
				break
			}
		}
	}
err:
	if b != nil {
		p.freeBuffers.push(b, true)
	}
	p.ctrlChan <- CTRL_CMD_EXIT
	p.wg.Done()
}

//数据组装函数
//@cmd 发送命令， PROXY_CMD_XX
//@b 待发达缓存，不为空则利用该缓存，nil则重新分配；
//@body 待发送数据，不为空则将数据写入缓存数据区
func (p *Proxy) sendCommand(subtype bool, id uint32, cmd byte, b *buffer, body []byte) {
	//头部占用8字节
	if b == nil {
		b = p.getBuffer()
		if b == nil {
			fmt.Printf("allocate new buffer failed, exit.\n")
			return
		}
		b.size = 8
	}
	//data[0]低5位表示PROXY_CMD_XX命令
	b.data[0] = cmd & 0x1f
	if subtype {
		b.data[0] |= 0x80
	}
	//data[4-7]为连接ID，小端
	b.data[4] = byte(id)
	b.data[5] = byte(id >> 8)
	b.data[6] = byte(id >> 16)
	b.data[7] = byte(id >> 24)
	if body != nil && len(body) > 0 {
		copy(b.data[8:len(body)+8], body)
		b.size = 8 + len(body)
	}
	//data[2-3]为加密后数据大小，含头部
	//data[1]低4位为aes128加密时补齐字节数
	p.encryptBuffer(b)
	//尽快发送，如发送不及时，在此处阻塞
	if cmd == PROXY_CMD_PAUSE || cmd == PROXY_CMD_RUN {
		p.emergencyChan <- b
	} else {
		p.sendChan <- b
	}
}

//子连接发送命令/数据函数
func (p *Proxy) clientSendCommand(cli *client, cmd byte, b *buffer, body []byte) {
	p.sendCommand(cli.subtype, cli.id, cmd, b, body)
}

//将缓存写入socket
//@b 待写入缓存
func (p *Proxy) writeBuffer(b *buffer) bool {
	offset := 0
	if b.size > DEFAULT_BUFFER_SIZE {
		panic("buffer size error")
	}
	for {
		cnt, err := p.c.Write(b.data[offset:b.size])
		if err != nil {
			fmt.Printf("write with error:%s.\n", err.Error())
			return false
		}
		if cnt+offset == b.size {
			break
		}
		offset += cnt
		continue
	}
	//释放缓存，如有必要，可以清理
	p.freeBuffers.append(b, true)
	return true
}


//主连接写和事件处理go程
//1. 如发生各种事件，向ctrlChan发送命令字
//2. 如有数据需要发送，向sendChan发送buffer指针
//3. 应急数据向emergencyChan发送buffer指针
func (p *Proxy) write() {
	go p.read()
	var b *buffer = nil
	tickms := 0
	for {
		select {
		case cmd := <-p.ctrlChan:
			switch cmd {
			case CTRL_CMD_EXIT:
				goto err
			case CTRL_CMD_TICK:
				//定时释放空闲内存， read go程超时产生定时器
				if b := p.freeBuffers.pop(); b != nil {
					p.bp.put(b)
					b = nil
				}
				tickms += (int(TICK_MS) / 1000000)
				if tickms > 60000 {
					tickms -= 60000
					p.sendCommand(false, 0, PROXY_CMD_KEEPALIVE, nil, nil)
				}

				if time.Now().Unix() - p.keepaliveAt > 120 {
					goto err
				}
			}
		case b = <-p.emergencyChan:
		case b = <-p.sendChan:
		}
		if b != nil {
			if ok := p.writeBuffer(b); !ok {
				goto err
			}
			b = nil
		}
	}
err:
	p.c.Close()
	p.mutex.Lock()
	for _, lsn := range p.listeners {
		if lsn.l != nil {
			lsn.active = false
			_ = lsn.l.Close()
		}
	}
	for _, cli := range p.clients {
		select {
		case cli.ctrlChan <- CTRL_CMD_FORCE_EXIT:
		default:
		}
	}
	for _, cli := range p.subClients {
		select {
		case cli.ctrlChan <- CTRL_CMD_FORCE_EXIT:
		default:
		}
	}
	p.mutex.Unlock()
	p.wg.Wait()
	p.bp.appendList(p.freeBuffers)
	finish := false
	for !finish {
		select {
		case b := <-p.emergencyChan:
			p.bp.put(b)
		case b := <-p.sendChan:
			p.bp.put(b)
		default:
			finish = true
		}
	}
	p.exitCB(p)
}

//代理处理函数
//@c 主连接，用于承载服务器之间数据传输，需要完成必要的认证和aes128密钥分配
//由读go程负责读取，读入数据都经aes128加密，需要解密后发送给子连接或监听子连接
//由主连接发送的数据都需要aes128加密，子连接或监听子连接发送数据时通过sendChan传入主线程
//发送或接受的包大小受buffer限制，大于buffer限制的包需要手动分包
func (p *Proxy) Handle() {
	p.write()
}


//主动退出函数
//@c 主连接，用于承载服务器之间数据传输，需要完成必要的认证和aes128密钥分配
//由读go程负责读取，读入数据都经aes128加密，需要解密后发送给子连接或监听子连接
//由主连接发送的数据都需要aes128加密，子连接或监听子连接发送数据时通过sendChan传入主线程
//发送或接受的包大小受buffer限制，大于buffer限制的包需要手动分包
func (p *Proxy) Close() {
	select {
	case p.ctrlChan <- CTRL_CMD_EXIT:
	default:
	}
}