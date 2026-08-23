// importcookies loads a Netscape-format cookies.txt file (e.g. exported from a browser via the
// Cookie-Editor extension) into ogamed's persistent cookie jar for a given device, so ogamed can
// reuse an already-established browser session instead of performing its own login handshake.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/alaingilbert/ogame/pkg/device"
	cookiejar "github.com/orirawlings/persistent-cookiejar"
)

func main() {
	deviceName := flag.String("device-name", "", "device name (must match ogamed's --device-name)")
	cookiesFile := flag.String("cookies-file", "", "path to the Netscape-format cookies.txt file")
	flag.Parse()

	if *deviceName == "" || *cookiesFile == "" {
		fmt.Fprintln(os.Stderr, "usage: importcookies --device-name=<name> --cookies-file=<path>")
		os.Exit(1)
	}

	cookies, err := parseNetscapeCookiesFile(*cookiesFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to parse cookies file:", err)
		os.Exit(1)
	}

	jarPath := filepath.Join(device.DefaultStoragePath(), *deviceName, "cookies")
	if err := os.MkdirAll(filepath.Dir(jarPath), 0755); err != nil {
		fmt.Fprintln(os.Stderr, "failed to create storage dir:", err)
		os.Exit(1)
	}

	jar, err := cookiejar.New(&cookiejar.Options{Filename: jarPath, PersistSessionCookies: true})
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to open cookie jar:", err)
		os.Exit(1)
	}

	byDomain := map[string][]*http.Cookie{}
	for _, c := range cookies {
		byDomain[c.domain] = append(byDomain[c.domain], c.toHTTPCookie())
	}
	for domain, domainCookies := range byDomain {
		u := &url.URL{Scheme: "https", Host: strings.TrimPrefix(domain, ".")}
		jar.SetCookies(u, domainCookies)
	}

	if err := jar.Save(); err != nil {
		fmt.Fprintln(os.Stderr, "failed to save cookie jar:", err)
		os.Exit(1)
	}

	fmt.Printf("Imported %d cookies into %s\n", len(cookies), jarPath)
}

type netscapeCookie struct {
	domain, path, name, value string
	secure                    bool
	expires                   time.Time
}

func (c netscapeCookie) toHTTPCookie() *http.Cookie {
	return &http.Cookie{
		Name:    c.name,
		Value:   c.value,
		Domain:  c.domain,
		Path:    c.path,
		Secure:  c.secure,
		Expires: c.expires,
	}
}

// parseNetscapeCookiesFile parses the tab-separated Netscape cookie file format
// (domain, includeSubdomains, path, secure, expires, name, value).
func parseNetscapeCookiesFile(path string) ([]netscapeCookie, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var cookies []netscapeCookie
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 7 {
			continue
		}
		expiresUnix, _ := strconv.ParseInt(fields[4], 10, 64)
		cookies = append(cookies, netscapeCookie{
			domain:  fields[0],
			path:    fields[2],
			secure:  fields[3] == "TRUE",
			expires: time.Unix(expiresUnix, 0),
			name:    fields[5],
			value:   fields[6],
		})
	}
	return cookies, scanner.Err()
}
