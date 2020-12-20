package main

import (
	"fmt"
	job "github.com/AgentCoop/go-work"
	"github.com/jessevdk/go-flags"
	"math/rand"
	"net"
	"os"
)

func (b *TcpBalancer) inConnHandler(conn *net.TCPConn) {
	u := &Upstream{}
	u.Source = make(chan []byte)
	u.Sink = make(chan []byte)
	u.ClientConn = conn
	// Select random upstream server
	randIndex := rand.Intn(len(b.UpstreamServers))
	u.UpstreamServer = b.UpstreamServers[randIndex]

	j := job.NewJob(u)
	j.WithPrerequisites(u.connect())
	j.AddTask(upstreamWrite)
	j.AddTask(downstreamWrite)
	j.AddTask(upstreamRead)
	j.AddTask(downstreamRead)
	fmt.Printf(" -> forward conn to %s\n", u.UpstreamServer.TcpAddr)
	<-j.Run()
}

func main() {
	parser := flags.NewParser(&CliOptions, flags.PassDoubleDash | flags.PrintErrors)
	parser.ParseArgs(os.Args)

	if CliOptions.Port == 0 {
		fmt.Printf("Specify TCP port to listen on\n")
		os.Exit(-1)
	}

	balancer := &TcpBalancer{}
	balancerJob := job.NewJob(balancer)
	balancerJob.AddTask(upstreamResolver)
	balancerJob.AddTask(connListener)

	<-balancerJob.Run()
}
