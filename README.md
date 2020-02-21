## 关于goproxy

goproxy是基于golang开发的简单易用代理库和程序，由github.com/idste/goproxy/proxy包和apps样例程序组成，实现端口映射，支持TCP协议，用于端口映射、NAT空越等应用。

## 原理

goproxy将tcp连接分为主连接和子连接，主连接用于两个goproxy实例之间收发数据，子连接用于完成客户端与goproxy实例之间的数据收发。子连接接收的数据由主连接传输至对端goproxy实例，相应地，goproxy实例接收的数据由子连接发送至客户端。

goproxy的输入主要是一个主连接和aes128密钥，主连接的认证和aes128密钥分发逻辑由应用自己处理，请参考apps/node/nodes.go或apps/server/server.go代码。主连接传输的数据都经随机生成并由rsa2048密钥加密传输至对端的aes128密钥加密，保证数据安全和完整。

goproxy包是**简单**和**对称**的，库代码约为1000行，服务端和客户端都是goproxy实例，具有相同的逻辑，唯一不同的是认证逻辑和监听&转发接口调用（NewListener和NewPeerListener接口）不同。

## 系统架构

![architecture](/doc/pic/goproxy.png)

如图所示，整个系统由goproxy实例、主连接、listener监听、client转发子连接和subclient监听子连接组成。

## 交互过程

- 建立监听: 当实例A接收到监听命令时在监听地址上监听连接，如收到示例中的监听命令时会在0.0.0.0:1080地址上监听。监听示例：
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
- 建立监听子连接：实例A在监听地址上产生监听子连接时为其分配唯一ID，然后向实例B发送新连接建立命令并携带连接ID和转发地址127.0.0.1:80
- 建立代理子连接：实例B建立到127.0.0.1:80的转发子连接并设置连接ID
- 数据转发：监听子连接或转发子连接上接收到的数据时，通过消息转发命令发送至对端实例，由对端实例转发至最终目的地
- 关闭连接：监听子连接或转发子连接断开时，向对端实例发送断开连接命令并由对端实例关闭相应ID的连接

## 使用说明

从本项目apps目录下有node客户端和server服务端实例，实现了端口映射功能，既是给开发的使用参考也是实用工具，基于server和node可以实现内网穿越功能。

请从 [Release](https://github.com/idste/goproxy/releases) 下载最新版本程序或下载源码后手动编译。

为确保安全可靠，所有用户信息和监听转发地址都需要在服务端预先配置，客户端输入相关信息后由服务端鉴权判定是否可以接入，如果账号密码验证失败，则会立即关闭连接。

node需要填写目标服务器IP地址、端口、账号和密码，node客户端使用帮助：
```
  -host string
        proxy host 代理服务器地址 (default "127.0.0.1")    //指向server程序所在主机IP或域名
  -password string
        password (default "1e4d4e53556a1bb5f6adf4753e7956cb") //与uuid配对使用，用于连接认证
  -port int
        proxy port 代理端口 (default 925)
  -uuid string
        UUID (default "idste")                            //用于连接认证，建议为其随机分配一个32字节的字串
```

server可以配置服务监听地址和端口，如果仅是单一客户端应用，可以在程序命令行参数中配置默认账号密码和监听转发地址，服务于多个客户端时请使用配置文件。server服务端使用帮助:
```
  -config_path string
    	config file (default "/etc/goproxy.conf")
  -host string
        listen host ip代理服务监听地址 (default "0.0.0.0")
  -listener value
        listen&forward address list代理端监听转发地址，可多次传入该参数    //"本端在指定地址上监听并由对端转发至目的地"方式的地址信息
  -password string
        password (default "1e4d4e53556a1bb5f6adf4753e7956cb")              //与uuid配对使用，用于非配置文件管理的用户连接认证
  -peer_listener value
        peer listen&forward address list内网代理转发地址，可多次传入该参数 //"对端在指定地址上监听并由本端转发至目的地"方式的地址信息
  -port int
        listen port代理服务监听端口 (default 925)
  -uuid string
        UUID (default "idste")
```

`-listener`参数示例：`-listener '{"Listen":{"Domain":"tcp","Addr":"127.0.0.1:1080"},"Forward":{"Domain":"tcp", "Addr":"127.0.0.1:80"}}'` 表示server在127.0.0.1:1080监听，数据转发至node端127.0.0.1:80

`-peer_listener`参数示例:`-peer_listener '{"Listen":{"Domain":"tcp","Addr":"127.0.0.1:1022"},"Forward":{"Domain":"tcp", "Addr":"127.0.0.1:22"}}'` 表示node在127.0.0.0:1022监听，数据转发至server端127.0.0.1:22

## 配置文件示例

如果你的服务端有多个客户端接入，请使用配置文件描述客户端信息，默认配置文件为/etc/goproxy.conf，也可使用-config_path指明需要使用的配置文件。

配置文件为json格式，clients对象包含所有可用客户端对象数据，每个客户端对象数据由uuid/password/listen/peerListen组成，uuid用于标识客户端，password用于加密数据，listen存储代理方式为"服务端监听端口并由客户端转发至目的地"的地址信息，peerListen存储代理方式为"客户端监听端口并由服务端转发至目的地"的地址信息
```json
{
    "clients":[
        {
            "uuid":"testclient1",
            "password":"client1_password",
            "listen":[
                {
                    "Listen":{
                        "Domain":"tcp",
                        "Addr":"0.0.0.0:8080"
                    },
                    "Forward":{
                        "Domain":"tcp",
                        "Addr":"127.0.0.1:80"
                    }
                }
            ],
            "peerListen":[
                {
                    "Listen":{
                        "Domain":"tcp",
                        "Addr":"0.0.0.0:8022"
                    },
                    "Forward":{
                        "Domain":"tcp",
                        "Addr":"127.0.0.1:22"
                    }
                }
            ]
        },
        {
            "uuid":"testclient2",
            "password":"client2_password",
            "listen":[
                {
                    "Listen":{
                        "Domain":"tcp",
                        "Addr":"0.0.0.0:8081"
                    },
                    "Forward":{
                        "Domain":"tcp",
                        "Addr":"127.0.0.1:80"
                    }
                }
            ],
            "peerListen":[
                {
                    "Listen":{
                        "Domain":"tcp",
                        "Addr":"0.0.0.0:8022"
                    },
                    "Forward":{
                        "Domain":"tcp",
                        "Addr":"127.0.0.1:22"
                    }
                }
            ]
        }
    ]
}
```

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