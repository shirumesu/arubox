package platform

/*
#cgo LDFLAGS: -framework CoreFoundation -framework CFNetwork
#include <CoreFoundation/CoreFoundation.h>
#include <CFNetwork/CFNetwork.h>

// proxySetting reads one host and port pair from the system proxy settings.
// It returns false when that kind of proxy is turned off.
static bool proxySetting(CFDictionaryRef settings, CFStringRef enableKey, CFStringRef hostKey, CFStringRef portKey, char *host, int hostSize, int *port) {
	CFNumberRef enabled = CFDictionaryGetValue(settings, enableKey);
	int on = 0;
	if (enabled == NULL || !CFNumberGetValue(enabled, kCFNumberIntType, &on) || !on) {
		return false;
	}
	CFStringRef h = CFDictionaryGetValue(settings, hostKey);
	CFNumberRef p = CFDictionaryGetValue(settings, portKey);
	if (h == NULL || p == NULL || !CFStringGetCString(h, host, hostSize, kCFStringEncodingUTF8) || !CFNumberGetValue(p, kCFNumberIntType, port)) {
		return false;
	}
	return true;
}

// systemProxy fills in the HTTPS proxy, or else the SOCKS proxy. kind is 1
// for HTTPS, 2 for SOCKS and 0 when no proxy is set.
static int systemProxy(char *host, int hostSize, int *port) {
	CFDictionaryRef settings = CFNetworkCopySystemProxySettings();
	if (settings == NULL) {
		return 0;
	}
	int kind = 0;
	if (proxySetting(settings, kCFNetworkProxiesHTTPSEnable, kCFNetworkProxiesHTTPSProxy, kCFNetworkProxiesHTTPSPort, host, hostSize, port)) {
		kind = 1;
	} else if (proxySetting(settings, kCFNetworkProxiesSOCKSEnable, kCFNetworkProxiesSOCKSProxy, kCFNetworkProxiesSOCKSPort, host, hostSize, port)) {
		kind = 2;
	}
	CFRelease(settings);
	return kind;
}
*/
import "C"

import (
	"net"
	"net/http"
	"net/url"
	"strconv"
	"unsafe"
)

// SystemProxy returns the proxy the system settings name for HTTPS traffic,
// for use as http.Transport.Proxy. Automatic (PAC) configuration is not
// followed.
func SystemProxy(*http.Request) (*url.URL, error) {
	var host [256]C.char
	var port C.int
	switch C.systemProxy(&host[0], C.int(len(host)), &port) {
	case 1:
		return proxyURL("http", C.GoString((*C.char)(unsafe.Pointer(&host[0]))), int(port)), nil
	case 2:
		return proxyURL("socks5", C.GoString((*C.char)(unsafe.Pointer(&host[0]))), int(port)), nil
	}
	return nil, nil
}

func proxyURL(scheme, host string, port int) *url.URL {
	return &url.URL{Scheme: scheme, Host: net.JoinHostPort(host, strconv.Itoa(port))}
}
