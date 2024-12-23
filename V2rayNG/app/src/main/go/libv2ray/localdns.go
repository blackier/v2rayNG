package libv2ray

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"sync"
	"time"

	"golang.org/x/net/dns/dnsmessage"

	appdns "github.com/xtls/xray-core/app/dns"
	"github.com/xtls/xray-core/common/errors"
	"github.com/xtls/xray-core/common/net"
	featdns "github.com/xtls/xray-core/features/dns"
)

type record struct {
	A    *IPRecord
	AAAA *IPRecord
}

type IPRecord struct {
	ReqID  uint16
	IP     []net.Address
	Expire time.Time
	RCode  dnsmessage.RCode
}

var errRecordNotFound = errors.New("record not found")

var localNameServer *LocalNameServer

type LocalNameServer struct {
	sync.RWMutex
	ips map[string]*record
}

func LocalDNS(domain string, disableCache bool) (ips []net.IP, err error) {
	if localNameServer == nil {
		localNameServer, _ = NewLocalNameServer()
	}

	ips, err = localNameServer.QueryIP(domain, disableCache)

	if len(ips) > 1 {
		// rand ip
		rand.Seed(time.Now().UnixMilli())
		rand.Shuffle(len(ips), func(i, j int) {
			ips[i], ips[j] = ips[j], ips[i]
		})
	}

	return ips, err
}

func NewLocalNameServer() (*LocalNameServer, error) {

	s := &LocalNameServer{
		ips: make(map[string]*record),
	}

	return s, nil
}

func (s *LocalNameServer) updateIP(domain string, ips []net.IP) {
	s.Lock()
	rec, found := s.ips[domain]
	if !found {
		rec = &record{}
	}
	updated := false

	rec.A = &IPRecord{}
	rec.AAAA = &IPRecord{}

	for _, ip := range ips {
		address := net.IPAddress(ip)
		if address.Family() == net.AddressFamilyIPv6 {
			rec.AAAA.IP = append(rec.AAAA.IP, address)
		} else {
			rec.A.IP = append(rec.A.IP, address)
		}
		updated = true
	}

	if updated {
		s.ips[domain] = rec
	}
	s.Unlock()
}

func toNetIP(addrs []net.Address) ([]net.IP, error) {
	ips := make([]net.IP, 0, len(addrs))
	for _, addr := range addrs {
		if addr.Family().IsIP() {
			ips = append(ips, addr.IP())
		} else {
			return nil, fmt.Errorf("Failed to convert address %v to Net IP.", addr)
		}
	}
	return ips, nil
}

func (s *LocalNameServer) findIPsForDomain(domain string, option featdns.IPOption) ([]net.IP, error) {
	s.RLock()
	record, found := s.ips[domain]
	s.RUnlock()

	if !found {
		return nil, errRecordNotFound
	}

	var err4 error
	var err6 error
	var ips []net.Address
	var ip6 []net.Address

	if option.IPv4Enable {
		ips = record.A.IP
	}

	if option.IPv6Enable {
		ip6 = record.AAAA.IP
		ips = append(ips, ip6...)
	}

	if len(ips) > 0 {
		return toNetIP(ips)
	}

	if err4 != nil {
		return nil, err4
	}

	if err6 != nil {
		return nil, err6
	}

	return nil, featdns.ErrEmptyResponse
}

// QueryIP implements Server.
func (s *LocalNameServer) QueryIP(domain string, disableCache bool) (ips []net.IP, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var host string
	if host, _, err = net.SplitHostPort(domain); err != nil {
		host = domain
	}

	if !disableCache {
		ips, err = s.findIPsForDomain(host, featdns.IPOption{IPv4Enable: true, IPv6Enable: true})
		if err == nil {
			return ips, err
		}
		log.Printf("[local_dns] query IP cached failed, domain=%s, err=%v", domain, err)
	}

	ips, err = appdns.NewLocalDNSClient().QueryIP(ctx, host, featdns.IPOption{IPv4Enable: true, IPv6Enable: true}, false)

	if err == nil {
		s.updateIP(host, ips)
	}

	log.Printf("[local_dns] domain=%s, ips=%v, err=%v", domain, ips, err)

	return ips, err
}
