package conf

type Config struct {
	ListenAddress string // Address to listen on, passed to net.Listen
	ListenType    string // Type of network to listen on, passed to net.Listen. One of tcp, tcp4 and tcp6

}
