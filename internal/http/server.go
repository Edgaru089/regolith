package http

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"edgaru089.ink/go/regolith/internal/perm"
)

const (
	outgoing_client_timeout = 10 * time.Second
)

var (
	dialer      = net.Dialer{Timeout: outgoing_client_timeout}
	http_client = http.Client{Timeout: outgoing_client_timeout}
)

type Server struct {
	Perm *perm.Perm
}

// checkPerm invokes perm.Match.
// If s.perm is nil, then every request is Accept-ed.
//
// It also logs if the action is Deny.
func (s *Server) checkPerm(src, dest string) (act perm.Action) {
	if s.Perm == nil {
		return perm.ActionAccept
	}

	src_host, _, _ := net.SplitHostPort(src)
	act = s.Perm.Match(src_host, dest)
	if act == perm.ActionDeny {
		log.Printf("denied: [%s] -> [%s]", src, dest)
	}
	return
}

func (s *Server) Serve(listener net.Listener) (err error) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}

		go s.dispatch(conn)
	}
}

func (s *Server) dispatch(conn net.Conn) {
	buf := bufio.NewReader(conn)
	for {
		req, err := http.ReadRequest(buf)
		if err != nil {
			// Invalid request
			_ = conn.Close()
			break
		}

		if req.Method == http.MethodConnect {
			if buf.Buffered() > 0 {
				// There is still data in the buffered reader.
				// We need to get it out and put it into a cachedConn,
				// so that handleConnect can read it.
				data := make([]byte, buf.Buffered())
				_, err := io.ReadFull(buf, data)
				if err != nil {
					// Read from buffer failed, is this possible?
					_ = conn.Close()
					return
				}
				cachedConn := &cached_conn{
					Conn:   conn,
					Buffer: *bytes.NewBuffer(data),
				}
				s.handle_connect(cachedConn, req)
			} else {
				// No data in the buffered reader, we can just pass the original connection.
				s.handle_connect(conn, req)
			}
			// handle_connect will take over the connection,
			// i.e. it will not return until the connection is closed.
			// When it returns, there will be no more requests from this connection,
			// so we simply exit the loop.
			break
		} else {
			// handle_request on the other hand handles one request at a time,
			// and returns when the request is done. It returns a bool indicating
			// whether the connection should be kept alive, but itself never closes
			// the connection.
			kept_alive := s.handle_request(conn, req)
			if !kept_alive {
				_ = conn.Close()
				return
			}
		}
	}
}

// cached_conn is a net.Conn wrapper that first Read()s from a buffer,
// and then from the underlying net.Conn when the buffer is drained.
type cached_conn struct {
	net.Conn
	Buffer bytes.Buffer
}

func (c *cached_conn) Read(b []byte) (int, error) {
	if c.Buffer.Len() > 0 {
		n, err := c.Buffer.Read(b)
		if err == io.EOF {
			// Buffer is drained, hide it from the caller
			err = nil
		}
		return n, err
	}
	return c.Conn.Read(b)
}

// handle_connect returns until the connection is closed by
// the client, or errors. You don't need to close it again.
func (s *Server) handle_connect(conn net.Conn, req *http.Request) {
	conn.RemoteAddr()

	defer conn.Close()

	port := req.URL.Port()
	if port == "" {
		port = "80"
	}
	req_addr := net.JoinHostPort(req.URL.Hostname(), port)

	// check permission
	if s.checkPerm(conn.RemoteAddr().String(), req_addr) != perm.ActionAccept {
		_ = simple_respond(conn, req, http.StatusBadGateway)
		return
	}

	// prep for error log on close
	var close_err error
	defer func() {
		if close_err != nil && !errors.Is(close_err, net.ErrClosed) {
			// log non-closed errors
			log.Printf("[%s] -> [%s] error dialing remote: %v", conn.RemoteAddr(), req_addr, close_err)
		}
	}()

	// dial
	remote_conn, err := dialer.Dial("tcp", req_addr)
	if err != nil {
		simple_respond(conn, req, http.StatusBadGateway)
		close_err = err
		return
	}
	defer remote_conn.Close()

	log.Printf("[%s] -> [%s] connected", conn.RemoteAddr(), req_addr)
	// send a 200 OK and start copying
	_ = simple_respond(conn, req, http.StatusOK)
	err_chan := make(chan error, 2)
	go func() {
		_, err := io.Copy(remote_conn, conn)
		err_chan <- err
	}()
	go func() {
		_, err := io.Copy(conn, remote_conn)
		err_chan <- err
	}()
	close_err = <-err_chan
}

func (s *Server) handle_request(conn net.Conn, req *http.Request) (should_keepalive bool) {
	// Some clients use Connection, some use Proxy-Connection
	// https://www.oreilly.com/library/view/http-the-definitive/1565925092/re40.html
	keep_alive := req.ProtoAtLeast(1, 1) &&
		(strings.EqualFold(req.Header.Get("Proxy-Connection"), "keep-alive") ||
			strings.EqualFold(req.Header.Get("Connection"), "keep-alive"))
	req.RequestURI = "" // Outgoing request should not have RequestURI

	remove_hop_headers(req.Header)
	remove_extra_host_port(req)

	if req.URL.Scheme != "http" || req.URL.Host == "" {
		_ = simple_respond(conn, req, http.StatusBadRequest)
		return false
	}

	// Check permission
	req_hostname := req.URL.Hostname()
	req_port := req.URL.Port()
	if req_port == "" {
		req_port = "80"
	}
	if s.checkPerm(conn.RemoteAddr().String(), net.JoinHostPort(req_hostname, req_port)) != perm.ActionAccept {
		_ = simple_respond(conn, req, http.StatusBadGateway)
		return false
	}

	// Request & error log
	var close_err error
	defer func() {
		if close_err != nil && !errors.Is(close_err, net.ErrClosed) {
			// log non-closed errors
			log.Printf("[%s] -> [%s] error: %v", conn.RemoteAddr(), req.URL, close_err)
		}
	}()

	// Do the request and send the response back
	resp, err := http_client.Do(req)
	if err != nil {
		close_err = err
		_ = simple_respond(conn, req, http.StatusBadGateway)
		return false
	}

	remove_hop_headers(resp.Header)
	if keep_alive {
		resp.Header.Set("Connection", "keep-alive")
		resp.Header.Set("Proxy-Connection", "keep-alive")
		resp.Header.Set("Keep-Alive", "timeout=60")
	}

	close_err = resp.Write(conn)
	return close_err == nil && keep_alive
}

func remove_hop_headers(header http.Header) {
	header.Del("Proxy-Connection") // Not in RFC but common
	// https://www.ietf.org/rfc/rfc2616.txt
	header.Del("Connection")
	header.Del("Keep-Alive")
	header.Del("Proxy-Authenticate")
	header.Del("Proxy-Authorization")
	header.Del("TE")
	header.Del("Trailers")
	header.Del("Transfer-Encoding")
	header.Del("Upgrade")
}

func remove_extra_host_port(req *http.Request) {
	host := req.Host
	if host == "" {
		host = req.URL.Host
	}
	if pHost, port, err := net.SplitHostPort(host); err == nil && port == "80" {
		host = pHost
	}
	req.Host = host
	req.URL.Host = host
}

func simple_respond(conn net.Conn, req *http.Request, statusCode int) error {
	resp := &http.Response{
		StatusCode: statusCode,
		Status:     http.StatusText(statusCode),
		Proto:      req.Proto,
		ProtoMajor: req.ProtoMajor,
		ProtoMinor: req.ProtoMinor,
		Header:     http.Header{},
	}
	// Remove the "Content-Length: 0" header, some clients (e.g. ffmpeg) may not like it.
	resp.ContentLength = -1
	// Also, prevent the "Connection: close" header.
	resp.Close = false
	resp.Uncompressed = true
	return resp.Write(conn)
}
