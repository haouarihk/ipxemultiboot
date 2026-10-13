// ipxemultiboot is a network boot server: drop ISO files in a folder and boot
// them over PXE on any machine in the LAN.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"ipxemultiboot/internal/api"
	"ipxemultiboot/internal/app"
	"ipxemultiboot/internal/events"
	"ipxemultiboot/internal/httpd"
	"ipxemultiboot/internal/library"
	"ipxemultiboot/internal/store"
	"ipxemultiboot/web"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(key)); err == nil {
		return v
	}
	return def
}

func main() {
	var d app.Settings
	flag.StringVar(&d.ISODir, "iso-dir", env("IPXEMULTIBOOT_ISO_DIR", "./isos"), "default ISO folder [IPXEMULTIBOOT_ISO_DIR]")
	flag.StringVar(&d.Interface, "iface", env("IPXEMULTIBOOT_IFACE", ""), "default network interface, empty = auto [IPXEMULTIBOOT_IFACE]")
	flag.StringVar(&d.DHCPMode, "dhcp-mode", env("IPXEMULTIBOOT_DHCP_MODE", "proxy"), "default DHCP mode: proxy, full or bootstrap [IPXEMULTIBOOT_DHCP_MODE]")
	flag.StringVar(&d.PoolStart, "pool-start", env("IPXEMULTIBOOT_POOL_START", ""), "full mode: first address [IPXEMULTIBOOT_POOL_START]")
	flag.StringVar(&d.PoolEnd, "pool-end", env("IPXEMULTIBOOT_POOL_END", ""), "full mode: last address [IPXEMULTIBOOT_POOL_END]")
	flag.StringVar(&d.Router, "router", env("IPXEMULTIBOOT_ROUTER", ""), "full mode: default gateway [IPXEMULTIBOOT_ROUTER]")
	flag.StringVar(&d.DNS, "dns", env("IPXEMULTIBOOT_DNS", ""), "full mode: comma-separated DNS servers [IPXEMULTIBOOT_DNS]")
	flag.StringVar(&d.Language, "language", env("IPXEMULTIBOOT_LANGUAGE", "en"), "default UI language [IPXEMULTIBOOT_LANGUAGE]")
	dataDir := flag.String("data-dir", env("IPXEMULTIBOOT_DATA_DIR", "./data"), "database folder [IPXEMULTIBOOT_DATA_DIR]")
	httpPort := flag.Int("http-port", envInt("IPXEMULTIBOOT_HTTP_PORT", 8080), "HTTP port for boot files and the web interface [IPXEMULTIBOOT_HTTP_PORT]")
	tftpAddr := flag.String("tftp-addr", env("IPXEMULTIBOOT_TFTP_ADDR", ":69"), "TFTP listen address [IPXEMULTIBOOT_TFTP_ADDR]")
	verbose := flag.Bool("v", os.Getenv("IPXEMULTIBOOT_DEBUG") != "", "debug logging [IPXEMULTIBOOT_DEBUG]")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}
	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

	if err := run(d, *dataDir, *httpPort, *tftpAddr); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(defaults app.Settings, dataDir string, httpPort int, tftpAddr string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := defaults.Validate(); err != nil {
		return fmt.Errorf("invalid defaults: %w", err)
	}
	st, err := store.Open(dataDir)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer st.Close()

	a := &app.App{
		Version:  version,
		HTTPPort: httpPort,
		TFTPAddr: tftpAddr,
		Defaults: defaults,
		Store:    st,
		Lib:      library.New(defaults.ISODir),
		Hub:      events.NewHub(500),
	}
	if err := a.Load(ctx); err != nil {
		return err
	}
	s := a.Settings()
	slog.Info("starting ipxemultiboot", "version", version, "iso-dir", s.ISODir, "data-dir", dataDir, "dhcp", s.DHCPMode)
	a.Start(ctx)

	mux := http.NewServeMux()
	httpd.NewBoot(a.Lib, a.Hub).Register(mux)
	api.New(a).Register(mux)
	mux.Handle("/", web.Handler())

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", httpPort),
		Handler:           httpd.LogRequests(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}()
	slog.Info("web interface", "url", fmt.Sprintf("http://%s:%d/", hostOrLocal(a), httpPort))
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		return err
	}
	return nil
}

func hostOrLocal(a *app.App) string {
	if ip := a.Status().ServerIP; ip != "" {
		return ip
	}
	return "localhost"
}
