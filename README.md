## 关于goproxy

goproxy是基于golang开发的代理程序，由github.com/idste/goproxy/proxy包和apps样例程序组成，实现端口映射，支持TCP协议，用于端口映射、NAT空越等应用。

## 原理

goproxy将连接分为主连接和子连接，主连接用于server和node之间收发数据，子连接则完成客户端数据收发。子连接接收的数据由主连接传输至对端，相应地，主连接接收的数据由子连接发送至客户端。

goproxy的输入主要是一个主连接和aes128密钥，主连接的认证和aes128密钥分发逻辑由应用自己处理，请参考apps/node/nodes.go或apps/server/server.go代码。主连接传输的数据都经aes128加密，保证数据安全和完整。

goproxy包是**简单**和**对称**的，库代码约为1000行，服务端和客户端使用相同的代码，具有相同的逻辑，唯一不同的是应用的认证逻辑和监听&转发接口调用（NewListener和NewPeerListener接口）不同。

## 系统架构

![architecture](/doc/pic/goproxy.png)

如图所示，整个系统由代理、主连接、listener监听、client转发子连接和subclient监听子连接组成。

## 交互过程

- 建立监听: 当代理A接收到监听命令时在监听地址上监听连接，如收到示例中的监听命令时会在0.0.0.0:1080地址上监听。监听示例：
    ```json
    {
      "Listen":{
        "Domain": "tcp",
        "Addr":"0.0.0.0:1080"
      },
      "Forward":{
        "Domain": "tcp",
        "Addr":"127.0.0.1:80"
      }
    }
    ```
- 建立监听子连接：代理A在监听地址上产生监听子连接时为其分配唯一ID，然后向代理B发送新连接建立命令并携带连接ID和转发地址127.0.0.1:80
- 建立代理子连接：代理B建立到127.0.0.1:80的转发子连接并设置连接ID
- 数据转发：监听子连接或转发子连接上接收到的数据时，通过消息转发命令发送至对端，由对端转发至最终目的地
- 关闭连接：监听子连接或转发子连接断开时，向对端发送断开连接命令并由对端关闭相应ID的连接

## 使用说明

从本项目 [Release](https://github.com/idste/goproxy/releases) 下载最新版本程序或下载源码手动编译。

node使用帮助：
```
  -host string
        proxy host 代理服务器地址 (default "127.0.0.1")    //指向server程序所在主机IP或域名
  -port int
        proxy port 代理端口 (default 925)
  -uuid string
        UUID (default "idste")                            //用于连接认证，建议为其随机分配一个32字节的字串
```

server使用帮助:

```
  -host string
        listen host ip代理服务监听地址 (default "0.0.0.0")
  -listener value
        listen&forward address list代理端监听转发地址，可多次传入该参数    //调用NewListener生成新的监听
  -peer_listener value
        peer listen&forward address list内网代理转发地址，可多次传入该参数 //调用NewPeerListener通知对端在指定地址上监听
  -port int
        listen port代理服务监听端口 (default 925)
  -uuid string
        UUID (default "idste")
```

`-listener`参数示例：`-listener '{"Listen":{"Domain":"tcp","Addr":"127.0.0.1:1080"},"Forward":{"Domain":"tcp", "Addr":"127.0.0.1:80"}}'` 表示server在127.0.0.1:1080监听，数据转发至node端127.0.0.1:80

`-peer_listener`参数示例:`-peer_listener '{"Listen":{"Domain":"tcp","Addr":"127.0.0.1:1022"},"Forward":{"Domain":"tcp", "Addr":"127.0.0.1:22"}}'` 表示node在127.0.0.0:1022监听，数据转发至server端127.0.0.1:22

## 应用示例

- windows 3389映射实现远程接入客户桌面
  
  1. 在windows客户机运行：`.\node.exe -host server.example.com -port 925 -uuid testuuid`
  2. 在windows服务机运行：`.\server.exe -host 0.0.0.0 -port 925 -uuid testuuid -listener '{"Listen":{"Domain":"tcp","Addr":"127.0.0.1:13389"},"Forward":{"Domain":"tcp", "Addr":"127.0.0.1:3389"}}'`
  3. 运行windows远程桌面连接，地址为127.0.0.1:13389即可远程登录客户桌面
  
- linux服务器本地端口映射客户机内部

  1. 在linux客户机运行: `./node -host server.example.com -port 925 -uuid testuuid`
  2. 在linux服务器运行: `./server -host 0.0.0.0 -port 925 -uuid testuuid -peer_listener '{"Listen":{"Domain":"tcp","Addr":"127.0.0.1:14151"},"Forward":{"Domain":"tcp", "Addr":"127.0.0.1:4151"}}'`
  3. 在linux客户机访问127.0.0.1:14151即可访问linux服务器的127.0.0.1:4151服务
  
## linux部署

建议使用supervisor控制程序运行：
1. 安装supervisor: apt install supervisor 
2. 复制node或server至/usr/sbin并确保有可执行权限
3. 创建/etc/supervisor/conf.d/proxy.conf,内容为:
    ```shell script
    ; /etc/supervisor/conf.d/proxy.conf
    
    [program:proxys]
    # node及其参数
    command         = /usr/sbin/node
    # 或server及其参数
    # command         = /usr/sbin/server
    user            = root
    stdout_logfile  = /var/log/proxy
    ```
4. service supervisor restart