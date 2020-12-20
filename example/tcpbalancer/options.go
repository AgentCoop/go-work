package main

var CliOptions struct {
	Port int `short:"p" description:"Listens on a port for inbound connections"`
	UpstreamServers []string `short:"u" description:"Upstream servers"`
}