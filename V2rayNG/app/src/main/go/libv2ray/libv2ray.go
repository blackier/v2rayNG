package libv2ray

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	mobasset "golang.org/x/mobile/asset"

	v2net "github.com/xtls/xray-core/common/net"
	v2filesystem "github.com/xtls/xray-core/common/platform/filesystem"
	v2core "github.com/xtls/xray-core/core"
	v2stats "github.com/xtls/xray-core/features/stats"
	v2serial "github.com/xtls/xray-core/infra/conf/serial"
	_ "github.com/xtls/xray-core/main/distro/all"
	v2internet "github.com/xtls/xray-core/transport/internet"

	v2applog "github.com/xtls/xray-core/app/log"
	v2commlog "github.com/xtls/xray-core/common/log"
	feature_dns "github.com/xtls/xray-core/features/dns"
)

const (
	v2Asset     = "xray.location.asset"
	xudpBaseKey = "xray.xudp.basekey"
)

var memoryAssets *MemoryAssets = nil

type MemoryAssets struct {
	sync.RWMutex
	assets map[string][]byte
}

// NopSeekCloser returns a ReadCloser with a no-op Close method wrapping
// the provided ReadSeeker r.
func NopSeekCloser(r io.ReadSeeker) io.ReadSeekCloser {
	return nopSeekCloser{r}
}

type nopSeekCloser struct {
	io.ReadSeeker
}

func (nopSeekCloser) Close() error { return nil }

/*
V2RayPoint V2Ray Point Server
This is territory of Go, so no getter and setters!
*/
type V2RayPoint struct {
	SupportSet   V2RayVPNServiceSupportsSet
	statsManager v2stats.Manager

	v2rayOP   sync.Mutex
	closeChan chan struct{}

	Vpoint    *v2core.Instance
	IsRunning bool

	DomainName           string
	ConfigureFileContent string
}

/*V2RayVPNServiceSupportsSet To support Android VPN mode*/
type V2RayVPNServiceSupportsSet interface {
	Setup(Conf string) int
	Prepare() int
	Shutdown() int
	Protect(int) bool
	OnEmitStatus(int, string) int
}

/*RunLoop Run V2Ray main loop
 */
func (v *V2RayPoint) RunLoop(prefIPv6 bool) (err error) {
	v.v2rayOP.Lock()
	defer v.v2rayOP.Unlock()
	//Construct Context

	if !v.IsRunning {
		v.closeChan = make(chan struct{})

		ips, err := LocalDNS(v.DomainName, false)
		if err != nil {
			log.Println("server resolve failed resolve, shutdown")
			v.StopLoop()
			v.SupportSet.Shutdown()
			return fmt.Errorf("server resolve failed, domain=%s", v.DomainName)
		} else {
			log.Printf("server resolved, domain=%s, ips=%v", v.DomainName, ips)
		}

		err = v.pointloop()
	}
	return
}

/*StopLoop Stop V2Ray main loop
 */
func (v *V2RayPoint) StopLoop() (err error) {
	v.v2rayOP.Lock()
	defer v.v2rayOP.Unlock()
	if v.IsRunning {
		close(v.closeChan)
		v.shutdownInit()
		v.SupportSet.OnEmitStatus(0, "Closed")
	}
	return
}

// Delegate Funcation
func (v *V2RayPoint) QueryStats(tag string, direct string) int64 {
	if v.statsManager == nil {
		return 0
	}
	counter := v.statsManager.GetCounter(fmt.Sprintf("outbound>>>%s>>>traffic>>>%s", tag, direct))
	if counter == nil {
		return 0
	}
	return counter.Set(0)
}

func (v *V2RayPoint) shutdownInit() {
	v.IsRunning = false
	v.Vpoint.Close()
	v.Vpoint = nil
	v.statsManager = nil
}

func (v *V2RayPoint) pointloop() error {
	log.Println("loading v2ray config")
	config, err := v2serial.LoadJSONConfig(strings.NewReader(v.ConfigureFileContent))
	if err != nil {
		log.Println(err)
		return err
	}

	log.Println("new v2ray core")
	v.Vpoint, err = v2core.New(config)
	if err != nil {
		v.Vpoint = nil
		log.Println(err)
		return err
	}
	v.statsManager = v.Vpoint.GetFeature(v2stats.ManagerType()).(v2stats.Manager)

	log.Println("start v2ray core")
	v.IsRunning = true
	if err := v.Vpoint.Start(); err != nil {
		v.IsRunning = false
		log.Println(err)
		return err
	}

	v.SupportSet.Prepare()
	v.SupportSet.Setup("")
	v.SupportSet.OnEmitStatus(0, "Running")
	return nil
}

func (v *V2RayPoint) MeasureDelay(url string) (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)

	go func() {
		select {
		case <-v.closeChan:
			// cancel request if close called during meansure
			cancel()
		case <-ctx.Done():
		}
	}()

	return measureInstDelay(ctx, v.Vpoint, url)
}

// InitV2Env set v2 asset path
func InitV2Env(envPath string, key string) {
	//Initialize asset API, Since Raymond Will not let notify the asset location inside Process,
	//We need to set location outside V2Ray
	if len(envPath) > 0 {
		os.Setenv(v2Asset, envPath)
	}
	if len(key) > 0 {
		os.Setenv(xudpBaseKey, key)
	}

	if memoryAssets == nil {
		log.Printf("create MemoryAssets")
		memoryAssets = &MemoryAssets{
			assets: map[string][]byte{},
		}
	}

	//Now we handle read, fallback to gomobile asset (apk assets)
	v2filesystem.NewFileReader = func(path string) (r io.ReadCloser, err error) {
		memoryAssets.RLock()
		content, found := memoryAssets.assets[path]
		memoryAssets.RUnlock()

		if !found {
			if _, e := os.Stat(path); os.IsNotExist(e) {
				_, file := filepath.Split(path)
				r, err = mobasset.Open(file)
			} else {
				r, err = os.Open(path)
			}
			if err != nil {
				return nil, err
			}
			content, err = io.ReadAll(r)
			if err != nil {
				log.Printf("read asset failed: %s", path)
				return nil, err
			}
			memoryAssets.Lock()
			memoryAssets.assets[path] = content
			memoryAssets.Unlock()
			log.Printf("local asset: %s", path)
		} else {
			log.Printf("cached asset: %s", path)
		}
		return NopSeekCloser(bytes.NewReader(content)), err
	}
}

func MeasureOutboundDelay(ConfigureFileContent string, url string) (int64, error) {
	config, err := v2serial.LoadJSONConfig(strings.NewReader(ConfigureFileContent))
	if err != nil {
		return -1, err
	}

	// dont listen to anything for test purpose
	config.Inbound = nil
	// config.App: (fakedns), log, dispatcher, InboundConfig, OutboundConfig, (stats), router, dns, (policy)
	// keep only basic features
	config.App = config.App[:5]

	inst, err := v2core.New(config)
	if err != nil {
		return -1, err
	}

	inst.Start()
	delay, err := measureInstDelay(context.Background(), inst, url)
	inst.Close()
	return delay, err
}

/*NewV2RayPoint new V2RayPoint*/
func NewV2RayPoint(s V2RayVPNServiceSupportsSet, adns bool) *V2RayPoint {
	// inject our own log writer
	v2applog.RegisterHandlerCreator(v2applog.LogType_Console,
		func(lt v2applog.LogType,
			options v2applog.HandlerCreatorOptions) (v2commlog.Handler, error) {
			return v2commlog.NewLogger(createStdoutLogWriter()), nil
		})

	// v2internet.RegisterDialerController(func(network, address string, fd uintptr) error {
	// 	ret := s.Protect(int(fd))
	// 	log.Printf("RegisterDialerController protect fd %t", ret)
	// 	return nil
	// })
	// v2internet.RegisterListenerController(func(network, address string, fd uintptr) error {
	// 	ret := s.Protect(int(fd))
	// 	log.Printf("RegisterListenerController protect fd %t", ret)
	// 	return nil
	// })
	v := V2RayPoint{
		SupportSet: s,
	}
	v2internet.UseAlternativeSystemDialer(&protectedDialer{
		protector: s,
		resolver: func(domain string) (ips []net.IP, err error) {
			c := v.Vpoint.GetFeature(feature_dns.ClientType()).(feature_dns.Client)
			ips, _, err = c.LookupIP(domain, feature_dns.IPOption{IPv4Enable: true, IPv6Enable: true})
			log.Printf("[feature_dns] domain=%s, ips=%v, err=%v", domain, ips, err)
			return ips, err
		},
	})
	return &v
}

/*
CheckVersionX string
This func will return libv2ray binding version and V2Ray version used.
*/
func CheckVersionX() string {
	var version = 27
	return fmt.Sprintf("Lib v%d, Xray-core v%s", version, v2core.Version())
}

func measureInstDelay(ctx context.Context, inst *v2core.Instance, url string) (int64, error) {
	if inst == nil {
		return -1, errors.New("core instance nil")
	}

	tr := &http.Transport{
		TLSHandshakeTimeout: 6 * time.Second,
		DisableKeepAlives:   true,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dest, err := v2net.ParseDestination(fmt.Sprintf("%s:%s", network, addr))
			if err != nil {
				return nil, err
			}
			return v2core.Dial(ctx, inst, dest)
		},
	}

	c := &http.Client{
		Transport: tr,
		Timeout:   12 * time.Second,
	}

	if len(url) <= 0 {
		url = "https://www.google.com/generate_204"
	}

	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	start := time.Now()
	resp, err := c.Do(req)
	if err != nil {
		return -1, err
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return -1, fmt.Errorf("status != 20x: %s", resp.Status)
	}
	resp.Body.Close()
	return time.Since(start).Milliseconds(), nil
}
