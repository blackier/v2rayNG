package libv2ray

import (
	"context"
	"fmt"
	"log"
	"net"

	"github.com/xtls/xray-core/common/errors"
	v2rayNet "github.com/xtls/xray-core/common/net"
	"github.com/xtls/xray-core/transport/internet"
	"golang.org/x/sys/unix"
)

const (
	// For incoming connections.
	TCP_FASTOPEN = 23 // nolint: golint,stylecheck
	// For out-going connections.
	TCP_FASTOPEN_CONNECT = 30 // nolint: golint,stylecheck
)

func bindAddr(fd uintptr, ip []byte, port uint32) error {
	setReuseAddr(fd)
	setReusePort(fd)

	var sockaddr unix.Sockaddr

	switch len(ip) {
	case net.IPv4len:
		a4 := &unix.SockaddrInet4{
			Port: int(port),
		}
		copy(a4.Addr[:], ip)
		sockaddr = a4
	case net.IPv6len:
		a6 := &unix.SockaddrInet6{
			Port: int(port),
		}
		copy(a6.Addr[:], ip)
		sockaddr = a6
	default:
		return fmt.Errorf("unexpected length of ip")
	}

	return unix.Bind(int(fd), sockaddr)
}

func applyOutboundSocketOptions(network string, address string, fd uintptr, config *internet.SocketConfig) error {
	if config.Mark != 0 {
		if err := unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_MARK, int(config.Mark)); err != nil {
			return errors.New("failed to set SO_MARK")
		}
	}

	if isTCPSocket(network) {
		switch config.Tfo {
		case int32(internet.SocketConfig_TProxy):
			if err := unix.SetsockoptInt(int(fd), unix.SOL_TCP, TCP_FASTOPEN_CONNECT, 1); err != nil {
				return fmt.Errorf("failed to set TCP_FASTOPEN_CONNECT=1, %v", err)
			}
		case int32(internet.SocketConfig_Off):
		case int32(internet.SocketConfig_Redirect):
			if err := unix.SetsockoptInt(int(fd), unix.SOL_TCP, TCP_FASTOPEN_CONNECT, 0); err != nil {
				return fmt.Errorf("failed to set TCP_FASTOPEN_CONNECT=0, %v", err)
			}
		}

		if config.TcpKeepAliveInterval > 0 || config.TcpKeepAliveIdle > 0 {
			if config.TcpKeepAliveInterval > 0 {
				if err := unix.SetsockoptInt(int(fd), unix.IPPROTO_TCP, unix.TCP_KEEPINTVL, int(config.TcpKeepAliveInterval)); err != nil {
					return fmt.Errorf("failed to set TCP_KEEPINTVL, %v", err)
				}
			}
			if config.TcpKeepAliveIdle > 0 {
				if err := unix.SetsockoptInt(int(fd), unix.IPPROTO_TCP, unix.TCP_KEEPIDLE, int(config.TcpKeepAliveIdle)); err != nil {
					return fmt.Errorf("failed to set TCP_KEEPIDLE, %v", err)
				}
			}
			if err := unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_KEEPALIVE, 1); err != nil {
				return fmt.Errorf("failed to set SO_KEEPALIVE, %v", err)
			}
		}
	}

	if config.Tproxy.IsEnabled() {
		if err := unix.SetsockoptInt(int(fd), unix.SOL_IP, unix.IP_TRANSPARENT, 1); err != nil {
			return fmt.Errorf("failed to set IP_TRANSPARENT, %v", err)
		}
	}

	return nil
}

func setReuseAddr(fd uintptr) error {
	if err := unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEADDR, 1); err != nil {
		return fmt.Errorf("failed to set SO_REUSEADDR, %v", err)
	}
	return nil
}

func setReusePort(fd uintptr) error {
	if err := unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEPORT, 1); err != nil {
		return fmt.Errorf("failed to set SO_REUSEPORT, %v", err)
	}
	return nil
}

func isTCPSocket(network string) bool {
	switch network {
	case "tcp", "tcp4", "tcp6":
		return true
	default:
		return false
	}
}

func isUDPSocket(network string) bool {
	switch network {
	case "udp", "udp4", "udp6":
		return true
	default:
		return false
	}
}

func hasBindAddr(sockopt *internet.SocketConfig) bool {
	return sockopt != nil && len(sockopt.BindAddress) > 0 && sockopt.BindPort > 0
}

func ApplySockopt(sockopt *internet.SocketConfig, dest v2rayNet.Destination, fd uintptr, ctx context.Context) {
	if err := applyOutboundSocketOptions(dest.Network.String(), dest.Address.String(), fd, sockopt); err != nil {
		log.Printf("failed to apply socket options, %v", err)
	}
	if dest.Network == v2rayNet.Network_UDP && hasBindAddr(sockopt) {
		if err := bindAddr(fd, sockopt.BindAddress, sockopt.BindPort); err != nil {
			log.Printf("failed to bind source address to %v", err)
		}
	}
}
