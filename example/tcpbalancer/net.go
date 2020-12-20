package main

import (
	"fmt"
	job "github.com/AgentCoop/go-work"
	"net"
	"strconv"
)

func upstreamWrite(j job.Job) (func(), func() interface{}, func()) {
	return nil, func() interface{} {
		u := j.GetValue().(*Upstream)
		select {
		case data := <- u.Source:
			_, err := u.UpstreamConn.Write(data)
			j.Assert(err)
		}
		return nil
	}, func() {

	}
}

func upstreamRead(j job.Job) (func(), func() interface{}, func()) {
	buf := make([]byte, 1024)
	return nil, func() interface{} {
			u := j.GetValue().(*Upstream)
			nRead, err := u.UpstreamConn.Read(buf)
			j.Assert(err)
			u.Sink <- buf[0:nRead]
			return nil
		}, func() {

		}
}

func downstreamWrite(j job.Job) (func(), func() interface{}, func()) {
	return nil, func() interface{} {
		u := j.GetValue().(*Upstream)
		select {
		case data := <-u.Sink:
			_, err := u.ClientConn.Write(data)
			j.Assert(err)
		}
		return nil
	}, func() {
		u := j.GetValue().(*Upstream)
		fmt.Printf(" -> close conn to client %s\n", u.ClientConn.RemoteAddr())
		u.ClientConn.Close()
	}
}

func downstreamRead(j job.Job) (func(), func() interface{}, func()) {
	buf := make([]byte, 1024)
	return nil, func() interface{} {
		u := j.GetValue().(*Upstream)
		nRead, err := u.ClientConn.Read(buf)
		j.Assert(err)
		u.Source <- buf[0:nRead]
		return nil
	}, func() {

	}
}

func (u *Upstream) connect() chan struct{} {
	ch := make(chan struct{}, 1)
	go func() {
		server := u.UpstreamServer
		upstreamConn, dialErr := net.DialTCP("tcp4", nil, server.TcpAddr)
		if dialErr != nil {
			panic(dialErr)
		}
		u.UpstreamConn = upstreamConn
		ch <- struct{}{}
	}()
	return ch
}

func upstreamResolver(j job.Job) (func(), func() interface{}, func()) {
	return nil, func() interface{} {
		balancer := j.GetValue().(*TcpBalancer)
		for _, v := range CliOptions.UpstreamServers {
			tcpAddr, err := net.ResolveTCPAddr("tcp4", v)
			if err != nil {
				continue
			}
			s := &Server{}
			sn := &ServerNet{
				Server:  s,
				TcpAddr: tcpAddr,
			}
			balancer.UpstreamServers = append(balancer.UpstreamServers, sn)
		}
		return true
	}, func() { }
}

func connListener(j job.Job) (func(), func() interface{}, func()) {
	var plis *net.TCPListener
	init := func() {
		tcpAddr, err := net.ResolveTCPAddr("tcp4", ":" + strconv.Itoa(CliOptions.Port))
		if err != nil {
			fmt.Printf("Failed %v %s\n", CliOptions.Port, strconv.Itoa(CliOptions.Port))
			panic(err)
		}
		plis, err = net.ListenTCP("tcp4", tcpAddr)
		if err != nil {
			panic(err)
		}
	}

	run := func() interface{} {
		pconn, acceptErr := plis.AcceptTCP()
		j.Assert(acceptErr)
		fmt.Printf(" -> new connection from %s\n", pconn.RemoteAddr().String())
		balancer := j.GetValue().(*TcpBalancer)
		go balancer.inConnHandler(pconn)
		return nil
	}

	cancel := func() { }
	fmt.Printf(" -> listening on %d port\n", CliOptions.Port)
	return init, run, cancel
}
