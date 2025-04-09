package libv2ray

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"time"

	v2rayNet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/features/dns"
	"github.com/xtls/xray-core/transport/internet"
	"golang.org/x/sys/unix"
)

type Protector interface {
	Protect(fd int) bool
}

type protectedDialer struct {
	protector  Protector
	resolver   func(domain string) ([]net.IP, error)
	selectedIP net.IP
}

func (dialer protectedDialer) Dial(ctx context.Context, source v2rayNet.Address, destination v2rayNet.Destination, sockopt *internet.SocketConfig) (conn net.Conn, err error) {
	if destination.Network == v2rayNet.Network_Unknown || destination.Address == nil {
		log.Println("connect to invalid destination")
	}

	var ips []net.IP
	if destination.Address.Family().IsDomain() {
		ips, err = dialer.resolver(destination.Address.Domain())
		if err == nil && len(ips) == 0 {
			err = dns.ErrEmptyResponse
		}
		if err != nil {
			return nil, err
		}
	} else {
		ips = append(ips, destination.Address.IP())
	}

	for i, ip := range ips {
		if i > 0 {
			if err == nil {
				break
			} else {
				log.Printf("dial system failed: %s", err)
			}
			log.Printf("trying next address: %s", ip.String())
		}
		destination.Address = v2rayNet.IPAddress(ip)
		conn, err = dialer.dial(ctx, source, destination, sockopt)
		dialer.selectedIP = ip
	}
	if err != nil {
		dialer.selectedIP = nil
	}

	return conn, err
}

func (dialer protectedDialer) DestIpAddress() net.IP {
	return dialer.selectedIP
}

func (dialer protectedDialer) dial(ctx context.Context, source v2rayNet.Address, destination v2rayNet.Destination, sockopt *internet.SocketConfig) (conn net.Conn, err error) {
	destIp := destination.Address.IP()
	fd, err := getFd(destination)
	if err != nil {
		return
	}

	if !dialer.protector.Protect(int(fd)) {
		log.Printf("fd protect failed: %d", fd)
		unix.Close(fd)
		return nil, errors.New("protect failed")
	}
	log.Printf("fd protect success: %d", fd)

	if sockopt != nil {
		ApplySockopt(sockopt, destination, uintptr(fd), ctx)
	}

	switch destination.Network {
	case v2rayNet.Network_TCP:
		var sockaddr unix.Sockaddr
		if destination.Address.Family().IsIPv4() {
			socketAddress := &unix.SockaddrInet4{
				Port: int(destination.Port),
			}
			copy(socketAddress.Addr[:], destIp)
			sockaddr = socketAddress
		} else {
			socketAddress := &unix.SockaddrInet6{
				Port: int(destination.Port),
			}
			copy(socketAddress.Addr[:], destIp)
			sockaddr = socketAddress
		}

		err = unix.Connect(fd, sockaddr)
	case v2rayNet.Network_UDP:
		if !hasBindAddr(sockopt) {
			err = unix.Bind(fd, &unix.SockaddrInet6{})
		}
	}
	if err != nil {
		unix.Close(fd)
		return
	}

	file := os.NewFile(uintptr(fd), "socket")
	if file == nil {
		return nil, errors.New("failed to connect to fd")
	}
	defer file.Close()

	switch destination.Network {
	case v2rayNet.Network_UDP:
		pc, err := net.FilePacketConn(file)
		if err == nil {
			destAddr, err := net.ResolveUDPAddr("udp", destination.NetAddr())
			if err != nil {
				return nil, err
			}
			conn = &packetConnWrapper{
				conn: pc,
				dest: destAddr,
			}
		}
	default:
		conn, err = net.FileConn(file)
	}

	return
}

func getFd(destination v2rayNet.Destination) (fd int, err error) {
	var af int
	if destination.Network == v2rayNet.Network_TCP && destination.Address.Family().IsIPv4() {
		af = unix.AF_INET
	} else {
		af = unix.AF_INET6
	}
	switch destination.Network {
	case v2rayNet.Network_TCP:
		fd, err = unix.Socket(af, unix.SOCK_STREAM, unix.IPPROTO_TCP)
	case v2rayNet.Network_UDP:
		fd, err = unix.Socket(af, unix.SOCK_DGRAM, unix.IPPROTO_UDP)
	case v2rayNet.Network_UNIX:
		fd, err = unix.Socket(af, unix.SOCK_STREAM, 0)
	default:
		err = fmt.Errorf("unknow network")
	}
	return
}

type packetConnWrapper struct {
	conn net.PacketConn
	dest net.Addr
}

func (c *packetConnWrapper) Close() error {
	return c.conn.Close()
}

func (c *packetConnWrapper) LocalAddr() net.Addr {
	return c.conn.LocalAddr()
}

func (c *packetConnWrapper) RemoteAddr() net.Addr {
	return c.dest
}

func (c *packetConnWrapper) Write(p []byte) (int, error) {
	return c.conn.WriteTo(p, c.dest)
}

func (c *packetConnWrapper) Read(p []byte) (int, error) {
	n, _, err := c.conn.ReadFrom(p)
	return n, err
}

func (c *packetConnWrapper) SetDeadline(t time.Time) error {
	return c.conn.SetDeadline(t)
}

func (c *packetConnWrapper) SetReadDeadline(t time.Time) error {
	return c.conn.SetReadDeadline(t)
}

func (c *packetConnWrapper) SetWriteDeadline(t time.Time) error {
	return c.conn.SetWriteDeadline(t)
}
