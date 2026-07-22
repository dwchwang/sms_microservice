package model

// Target is the metadata Monitoring needs to ping one server.
type Target struct {
	ServerID   string
	ServerName string
	IPv4       string
	TCPPort    int
}
