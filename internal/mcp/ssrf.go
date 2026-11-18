// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package mcp

import (
	"context"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// A team server connects only to web MCPs on the internet: never to itself,
// its private network or a cloud metadata address (169.254.169.254), however
// the address is written.

var cgnat = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

func blockedIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsUnspecified() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || cgnat.Contains(ip) || (ip.To4() != nil && ip.To4()[0] == 0)
}

// literalIP reads the host as an address, including the forms inet_aton
// accepts: 127.1, 0x7f.1, 017700000001, 2130706433.
func literalIP(h string) net.IP {
	h = strings.Trim(h, "[]")
	if ip := net.ParseIP(h); ip != nil {
		return ip
	}
	parts := strings.Split(h, ".")
	if len(parts) == 0 || len(parts) > 4 {
		return nil
	}
	nums := make([]uint64, len(parts))
	for i, p := range parts {
		n, err := strconv.ParseUint(p, 0, 32) // 0x.. hex, 0.. octal, else decimal
		if err != nil {
			return nil
		}
		nums[i] = n
	}
	var v uint64
	switch len(nums) {
	case 1:
		v = nums[0]
	case 2:
		v = nums[0]<<24 | nums[1]
	case 3:
		v = nums[0]<<24 | nums[1]<<16 | nums[2]
	case 4:
		v = nums[0]<<24 | nums[1]<<16 | nums[2]<<8 | nums[3]
	}
	if v > 0xffffffff {
		return nil
	}
	return net.IPv4(byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}

// hostRefusal checks a URL's host without contacting anything.
func hostRefusal(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "not a URL"
	}
	h := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if h == "" {
		return "not a URL"
	}
	if h == "localhost" || strings.HasSuffix(h, ".localhost") || strings.HasSuffix(h, ".internal") || strings.HasSuffix(h, ".local") {
		return "a team server connects only to web MCPs on the internet, not to itself or its network"
	}
	if ip := literalIP(h); ip != nil && blockedIP(ip) {
		return "a team server connects only to web MCPs on the internet, not to itself or its network"
	}
	return ""
}

// resolveRefusal looks the host up and refuses if any address is local or private.
func resolveRefusal(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "not a URL"
	}
	h := u.Hostname()
	if literalIP(h) != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, h)
	if err != nil {
		return "" // Hermes cannot reach it either; the probe says so
	}
	for _, a := range addrs {
		if blockedIP(a.IP) {
			return h + " points at a local or private address; a team server connects only to web MCPs on the internet"
		}
	}
	return ""
}

// SafeURL drops user info and the query (where keys often ride) for display and logs.
func SafeURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	u.User, u.RawQuery, u.Fragment = nil, "", ""
	return u.String()
}
