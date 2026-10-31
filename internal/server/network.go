package server

import (
	"context"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

// lanIP returns this computer's address on the local network (Wi-Fi, Ethernet or a phone's hotspot),
// or "" when it isn't connected to one. VPN addresses (e.g. Tailscale's 100.x) are ignored.
func lanIP() string {
	// The address the OS would use for outside traffic, if it's a local-network one.
	if conn, err := net.Dial("udp", "8.8.8.8:80"); err == nil {
		ip := conn.LocalAddr().(*net.UDPAddr).IP
		conn.Close()
		if ip.IsPrivate() {
			return ip.String()
		}
	}
	// No route out (e.g. a hotspot without internet): take any local-network address.
	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagPointToPoint != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil && n.IP.IsPrivate() {
				return n.IP.String()
			}
		}
	}
	return ""
}

// localName returns this computer's Bonjour/mDNS name (e.g. "Laydens-MacBook-Pro.local"), or "" when
// that name doesn't resolve to ip, in which case the remote's address falls back to the IP.
// Checked at most every 30 s, since the lookup can take a moment.
func localName(ip string) string {
	nameMu.Lock()
	defer nameMu.Unlock()
	if ip == nameIP && time.Since(nameAt) < 30*time.Second {
		return nameCached
	}
	nameIP, nameAt, nameCached = ip, time.Now(), ""
	host := ""
	if runtime.GOOS == "darwin" {
		// os.Hostname on a Mac is often the router's name for it (e.g. "Mac.attlocal.net").
		if out, err := exec.Command("scutil", "--get", "LocalHostName").Output(); err == nil {
			host = strings.TrimSpace(string(out))
		}
	}
	if host == "" {
		h, _ := os.Hostname()
		host, _, _ = strings.Cut(h, ".")
	}
	if host == "" {
		return ""
	}
	name := host + ".local"
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ips, _ := net.DefaultResolver.LookupIP(ctx, "ip4", name)
	for _, a := range ips {
		if a.String() == ip {
			nameCached = name
		}
	}
	return nameCached
}

var (
	nameMu             sync.Mutex
	nameIP, nameCached string
	nameAt             time.Time
)
