package main

import (
	"bytes"
	"crypto/aes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
	"github.com/idste/goproxy/proxy"
)

type Server struct {
	active     bool
	id         uint32
	listenAddr proxy.Address
	mutex      sync.RWMutex
	l          net.Listener
	proxys     map[uint32]*proxy.Proxy
	bp         *proxy.BufferPool
	listeners  	  arg_list
	peerListeners arg_list
}

func serverProxyExit(p *proxy.Proxy) {
	s := p.Ctx.(*Server)
	s.mutex.Lock()
	defer s.mutex.Unlock()
	delete(s.proxys, p.ID)
}

//自定义登录过程，完成连接认证和aes128密钥下发
func (s *Server) login(c net.Conn) (p *proxy.Proxy, ok bool) {
	data := make([]byte, 4096)
	for {
		//读取标识"  v1"
		_ = c.SetReadDeadline(time.Now().Add(time.Second))
		if n, err := c.Read(data); err != nil || n < 4 {
			fmt.Println("Proxy read failed")
			break
		}
		if !bytes.Equal(data[0:4], []byte("  v1")) {
			fmt.Println("unknow flag:", string(data))
			break
		}
		if n, err := c.Write([]byte("hello")); err != nil || n < 5 {
			fmt.Println("write failed")
			break
		}

		//读取公钥
		_ = c.SetReadDeadline(time.Now().Add(time.Second))
		n, err := c.Read(data)
		if err != nil || n <= 0 {
			fmt.Println("Proxy read failed")
			break
		}
		cliPub, err := x509.ParsePKCS1PublicKey(data[0:n])
		if err != nil {
			fmt.Println("rsa public key parse failed")
			break
		}

		//生成公钥并发送给客户端
		srvKey, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			fmt.Println("general private key failed")
			break
		}
		pubstr := x509.MarshalPKCS1PublicKey(&srvKey.PublicKey)
		if _, err := c.Write(pubstr); err != nil {
			fmt.Println("write pubkey failed")
			break
		}
		//读取上传的节点UUID，该UUID用于识别客户端身份
		_ = c.SetReadDeadline(time.Now().Add(time.Second))
		k, err := c.Read(data)
		if err != nil || k <= 0 {
			fmt.Println("Proxy read failed")
			break
		}
		uuid, err := rsa.DecryptPKCS1v15(rand.Reader, srvKey, data[0:k])
		fmt.Printf("uuid:%s\n", uuid)
		if err != nil {
			fmt.Println("rsa decrypt failed:", err)
			break
		}
		//生成随机16字节随机确认字符和16字节aes密钥
		var aeskey [32]byte
		if _, err := io.ReadFull(rand.Reader, aeskey[0:32]); err != nil {
			fmt.Println("create aes key failed")
			break
		}
		aesBlock, err := aes.NewCipher(aeskey[16:32])
		if err != nil {
			fmt.Println("create new aes key failed:", err)
			break
		}
		//使用客户端公钥加密后下发
		msg, err := rsa.EncryptPKCS1v15(rand.Reader, cliPub, aeskey[0:32])
		if err != nil {
			fmt.Println("rsa pub encrypt failed")
			break
		}
		if _, err := c.Write(msg); err != nil {
			fmt.Println("write failed")
			break
		}
		//读取客户端的随机确认值字符并比对
		_ = c.SetReadDeadline(time.Now().Add(time.Second))
		if n, err := c.Read(data); err != nil || n < 16 {
			fmt.Println("Proxy read failed")
			break
		}
		aesBlock.Decrypt(data[0:16], data[0:16])
		if !bytes.Equal(data[0:16], aeskey[0:16]) {
			fmt.Println("aes key verify failed")
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
		p := proxy.NewProxy(s.id, c, s, aesBlock, s.bp, serverProxyExit)
		s.proxys[s.id] = p
		s.mutex.Unlock()
		return p, true
	}
	return nil, false
}

func (s *Server) handle(c net.Conn) {
	var p *proxy.Proxy = nil
	p, ok := s.login(c)
	if ok == false {
		_ = c.Close()
		fmt.Println("login failed, exit")
		return
	}
	go p.Handle()
	if len(s.listeners) == 0 {
		fmt.Printf("增加listen参数可设置本端监听地址， 示例-listener '{\"Listen\":{\"Domain\":\"tcp\",\"Addr\":\"127.0.0.1:1511\"},\"Forward\":{\"Domain\":\"tcp\", \"Addr\":\"127.0.0.1:80\"}}'\n")
	} else {
		//服务端监听示例：服务端监听127.0.0.1:1511连接，监听子连接上产生的数据转发至对端并由对端转发至127.0.0.1:80
		for _, v := range s.listeners {
			fmt.Printf("增加服务端监听，地址:%s\n", v)
			p.NewListener([]byte(v))
		}
	}

	if len(s.peerListeners) == 0 {
		//对端监听示例：对端监听127.0.0.1:1511，对端监听子连接上产生的数据转发至服务端端并由服务端转发至127.0.0.1:80
		fmt.Printf("增加peer_listen参数可添加对端监听地址， 示例-peer_listener '{\"Listen\":{\"Domain\":\"tcp\",\"Addr\":\"127.0.0.1:1511\"},\"Forward\":{\"Domain\":\"tcp\", \"Addr\":\"127.0.0.1:80\"}}'\n")
	} else {
		for _, v := range s.peerListeners {
			fmt.Printf("增加对端监听，地址:%s\n", v)
			p.NewPeerListener([]byte(v))
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
			fmt.Println("tcp listen failed:", err)
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

func NewServer(host string, port string, listeners arg_list, peerListeners arg_list) {
	s := &Server{active: true, listenAddr: proxy.Address{Domain: "tcp", Addr: host + ":" + port}, listeners:listeners, peerListeners:peerListeners}
	s.proxys = make(map[uint32]*proxy.Proxy)
	s.bp = proxy.NewBufferPool(10240)
	go s.newListen()
}
