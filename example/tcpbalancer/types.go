package main

import (
	"net"
)

type Host struct {
	Hostname 		string
	IpAddr 			net.IPAddr
	Port 			int
}

type Server struct {
	Host *Host
	Weight uint8
	MaxConns uint16
}

type ServerNet struct {
	Server *Server
	TcpAddr *net.TCPAddr
}

type Upstream struct {
	Source chan []byte
	Sink chan []byte

	SourceInbound func(data []byte) []byte
	SourceOutbound func(data []byte) []byte

	SinkInbound func(data []byte) []byte
	SinkOutbound func(data []byte) []byte

	UpstreamServer *ServerNet
	UpstreamConn *net.TCPConn
	ClientConn *net.TCPConn
}

type TcpBalancer struct {
	UpstreamServers []*ServerNet
}
