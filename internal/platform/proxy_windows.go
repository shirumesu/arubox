package platform

import (
	"net"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// SystemProxy returns the proxy the system settings name for HTTPS traffic,
// for use as http.Transport.Proxy. Automatic (PAC) configuration is not
// followed.
func SystemProxy(*http.Request) (*url.URL, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Internet Settings`, registry.QUERY_VALUE)
	if err != nil {
		return nil, nil
	}
	defer k.Close()
	if enabled, _, err := k.GetIntegerValue("ProxyEnable"); err != nil || enabled == 0 {
		return nil, nil
	}
	server, _, err := k.GetStringValue("ProxyServer")
	if err != nil {
		return nil, nil
	}
	return parseProxyServer(server), nil
}

// parseProxyServer reads ProxyServer, which is either "host:port" for every
// protocol or a list such as "http=host:port;https=host:port;socks=host:port".
func parseProxyServer(server string) *url.URL {
	server = strings.TrimSpace(server)
	if server == "" {
		return nil
	}
	if !strings.Contains(server, "=") {
		return hostURL("http", server)
	}
	byProtocol := map[string]string{}
	for entry := range strings.SplitSeq(server, ";") {
		if protocol, address, ok := strings.Cut(strings.TrimSpace(entry), "="); ok {
			byProtocol[strings.ToLower(protocol)] = address
		}
	}
	if address := byProtocol["https"]; address != "" {
		return hostURL("http", address)
	}
	if address := byProtocol["socks"]; address != "" {
		return hostURL("socks5", address)
	}
	return nil
}

func hostURL(scheme, address string) *url.URL {
	if _, _, err := net.SplitHostPort(address); err != nil {
		return nil
	}
	return &url.URL{Scheme: scheme, Host: address}
}
