package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
	"github.com/idste/goproxy/proxy"
)

type Node struct {
	active bool
	id     uint32
	mutex  sync.Mutex
	c      net.Conn
	proxys map[uint32]*proxy.Proxy
	bp     *proxy.BufferPool
	addr   proxy.Address
}

func nodeProxyExit(p *proxy.Proxy) {
	n := p.Ctx.(*Node)
	n.mutex.Lock()
	defer n.mutex.Unlock()
	delete(n.proxys, p.ID)
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
			fmt.Println("proxy write failed")
			break
		}
		//读取"hello"
		if n, err := c.Read(data); err != nil || n < 5 || bytes.Equal(data, []byte("hello")) {
			fmt.Println("can not get welcome replay")
			break
		}
		//生成rsa2048密钥并上传公钥
		rsakey, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			fmt.Println("generate rsa private key failed:", err)
			break
		}
		pubStr := x509.MarshalPKCS1PublicKey(&rsakey.PublicKey)
		if n, err := c.Write(pubStr); err != nil || n <= 0 {
			fmt.Println("proxy send public key failed")
			break
		}
		//读取服务器公钥
		if n, err := c.Read(data); err != nil || n <= 0 {
			fmt.Println("proxy read server key failed")
			break
		} else {
			rsaPub, err := x509.ParsePKCS1PublicKey(data[0:n])
			if err != nil || rsaPub == nil {
				fmt.Println("rsa server key parse failed")
				break
			}
			//加密uuid并上传，服务端使用uuid识别用户身份
			msg, err := rsa.EncryptPKCS1v15(rand.Reader, rsaPub, []byte("12345678901234567890123456789012"))
			if err != nil {
				fmt.Println("rsa pub encrypt failed")
			}
			if n, err := c.Write(msg); err != nil || n <= 0 {
				fmt.Println("proxy read failed 5")
				break
			}
		}
		var blk cipher.Block = nil
		//读取回应的aes密钥(前16字节为随机字符，后16字节为aes密钥)
		if n, err := c.Read(data); err != nil || n < 32 {
			fmt.Println("proxy read failed, n:", n)
			break
		} else {
			//使用公钥解密aes密钥
			aesKey, err := rsa.DecryptPKCS1v15(rand.Reader, rsakey, data[0:n])
			if err != nil {
				fmt.Println("rsa decrypt failed:", err)
				break
			}
			//使用aes密钥加密前16字节以确认登录交互完成
			aesBlk, err := aes.NewCipher(aesKey[16:32])
			if err != nil {
				fmt.Println("create aes block failed:", err)
				break
			}
			blk = aesBlk
			//使用aes加密前16字节随机确认字符
			blk.Encrypt(data[0:16], aesKey[0:16])
			if n, err := c.Write(data[0:16]); err != nil || n != 16 {
				fmt.Println("proxy write failed")
				break
			}
		}
		n.mutex.Lock()
		for {
			if _, ok := n.proxys[n.id]; ok == true {
				n.id++
			} else {
				break
			}
		}
		p := proxy.NewProxy(n.id, c, n, blk, n.bp, nodeProxyExit)
		n.proxys[n.id] = p
		n.mutex.Unlock()
		return p, true
	}
	return nil, false
}

func (n *Node) newConnect() {
	for {
		s := strings.Split(n.addr.Addr, ":")
		if len(s) != 2 {
			panic("error address:%" + n.addr.Addr + "\n")
		}
		ips, err := net.LookupHost(s[0])
		if err != nil {
			panic("unknow host:" + s[0] + "\n")
		}
		c, err := net.Dial(n.addr.Domain, ips[0]+":"+s[1])
		if err == nil {
			n.c = c
			//首先完成登录
			if p, ok := n.login(c); ok == true {
				go p.Handle()
				break
			}
		}
		fmt.Println("tcp connect failed:", err)
		time.Sleep(1 * time.Second)
	}
}

func NewNode(host string, port string) {
	n := &Node{active: true, addr: proxy.Address{Domain: "tcp", Addr: host + ":" + port}}
	n.proxys = make(map[uint32]*proxy.Proxy)
	n.bp = proxy.NewBufferPool(10240)
	go n.newConnect()
}