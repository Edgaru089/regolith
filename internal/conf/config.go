package conf

type Config struct {
	ListenAddress string // Address to listen on, passed to net.Listen
	ListenType    string // Type of network to listen on, passed to net.Listen. One of tcp, tcp4 and tcp6

	DNSResolver string // Address to send UDP & TCP DNS requests to. If set, the Go resolver will also be forced.
}
