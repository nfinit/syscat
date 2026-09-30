package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"syscall"
	"time"

	"syscat/internal/buildinfo"
	"syscat/internal/catalog"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	listen := flag.String("listen", ":8800", "address and port to listen on (default: all interfaces); an explicit -p/--port overrides this port")
	var port int
	flag.IntVar(&port, "port", 8800, "port to listen on (0 chooses an available port)")
	flag.IntVar(&port, "p", 8800, "shorthand for --port")
	dir := flag.String("data-dir", "./data", "directory for inventory and photographs")
	maxUploadMiB := flag.Int64("max-upload-mib", catalog.DefaultMaxUploadMiB, "maximum total upload request size in MiB")
	version := flag.Bool("version", false, "print application version and build commit, then exit")
	flag.Parse()
	if flag.NArg() != 0 {
		return fmt.Errorf("unexpected arguments; use --help for options")
	}
	if *version {
		fmt.Println(buildinfo.String())
		return nil
	}
	if *maxUploadMiB < 1 || *maxUploadMiB > (1<<63-1)>>20 {
		return fmt.Errorf("max-upload-mib must be a positive whole number within the supported byte range")
	}
	if port < 0 || port > 65535 {
		return fmt.Errorf("port must be between 0 and 65535")
	}
	portSet := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "port" || f.Name == "p" {
			portSet = true
		}
	})
	if portSet {
		host, _, err := net.SplitHostPort(*listen)
		if err != nil {
			return fmt.Errorf("invalid --listen address: %w", err)
		}
		*listen = net.JoinHostPort(host, strconv.Itoa(port))
	}
	absolute, err := filepath.Abs(*dir)
	if err != nil {
		return err
	}
	app, err := catalog.NewWithUploadLimit(absolute, *maxUploadMiB)
	if err != nil {
		return err
	}
	defer app.Close()
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		return err
	}
	server := &http.Server{Handler: app, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 15 * time.Minute, WriteTimeout: 15 * time.Minute, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	done := make(chan struct{})
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		select {
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if err := server.Shutdown(shutdown); err != nil {
				_ = server.Close()
			}
		case <-done:
		}
	}()
	log.Printf("%s is listening on %s", buildinfo.String(), listener.Addr())
	for _, url := range accessURLs(listener.Addr()) {
		log.Printf("Access URL: %s", url)
	}
	log.Printf("Data directory: %s", absolute)
	log.Printf("Maximum upload request: %d MiB", *maxUploadMiB)
	err = server.Serve(listener)
	close(done)
	<-shutdownDone
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

// accessURLs reports local interface addresses, not a public address behind NAT.
// Explicit bindings report only the bound address. Wildcard bindings offer
// loopback plus non-loopback addresses clients can use on the server's network.
func accessURLs(address net.Addr) []string {
	addr, ok := address.(*net.TCPAddr)
	if !ok {
		return nil
	}
	port := strconv.Itoa(addr.Port)
	if !addr.IP.IsUnspecified() && len(addr.IP) != 0 {
		host := addr.IP.String()
		if addr.Zone != "" {
			host += "%" + addr.Zone
		}
		return []string{"http://" + net.JoinHostPort(host, port)}
	}
	urls := []string{"http://" + net.JoinHostPort("127.0.0.1", port)}
	seen := map[string]bool{}
	interfaces, err := net.Interfaces()
	if err != nil {
		return urls
	}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addresses, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, address := range addresses {
			ip, _, err := net.ParseCIDR(address.String())
			if err != nil || !ip.IsGlobalUnicast() {
				continue
			}
			// An IPv4 wildcard listener cannot serve IPv6 interface addresses.
			if addr.IP.To4() != nil && ip.To4() == nil {
				continue
			}
			url := "http://" + net.JoinHostPort(ip.String(), port)
			if !seen[url] {
				urls = append(urls, url)
				seen[url] = true
			}
		}
	}
	sort.Strings(urls[1:])
	return urls
}
