// Package tftpd serves the embedded iPXE binaries over TFTP. Everything after
// iPXE is loaded goes over HTTP.
package tftpd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"strings"
	"time"

	"github.com/pin/tftp/v3"
)

type Server struct {
	addr    string
	files   fs.FS
	dynamic func(name string) ([]byte, bool) // generated files, e.g. autoexec.ipxe
	onSent  func(file, peer string, n int64, err error)
}

func New(addr string, files fs.FS, dynamic func(string) ([]byte, bool), onSent func(file, peer string, n int64, err error)) *Server {
	return &Server{addr: addr, files: files, dynamic: dynamic, onSent: onSent}
}

func (s *Server) sent(file, peer string, n int64, err error) {
	slog.Info("tftp: sent", "file", file, "bytes", n, "peer", peer, "err", err)
	if s.onSent != nil {
		s.onSent(file, peer, n, err)
	}
}

// AutoexecScript is served as autoexec.ipxe. iPXE fetches it from the
// next-server before using the DHCP filename, so chainloaded iPXE goes to the
// HTTP menu even when the DHCP server can't distinguish iPXE from firmware PXE
// (bootstrap mode behind a plain router).
func AutoexecScript(menuURL string) []byte {
	return []byte("#!ipxe\nchain --replace " + menuURL + "\n")
}

func (s *Server) read(filename string, rf io.ReaderFrom) error {
	name := strings.TrimLeft(filename, "/")
	peer := ""
	if t, ok := rf.(tftp.OutgoingTransfer); ok {
		addr := t.RemoteAddr()
		peer = addr.String()
	}
	if s.dynamic != nil {
		if b, ok := s.dynamic(name); ok {
			if t, ok := rf.(tftp.OutgoingTransfer); ok {
				t.SetSize(int64(len(b)))
			}
			n, err := rf.ReadFrom(bytes.NewReader(b))
			s.sent(name, peer, n, err)
			return err
		}
	}
	f, err := s.files.Open(name)
	if err != nil {
		slog.Warn("tftp: file not found", "file", filename, "peer", peer)
		return fmt.Errorf("not found: %s", filename)
	}
	defer f.Close()
	if st, err := f.Stat(); err == nil {
		if t, ok := rf.(tftp.OutgoingTransfer); ok {
			t.SetSize(st.Size())
		}
	}
	n, err := rf.ReadFrom(f)
	s.sent(name, peer, n, err)
	return err
}

// Run serves until ctx is cancelled.
func (s *Server) Run(ctx context.Context) error {
	srv := tftp.NewServer(s.read, nil)
	srv.SetTimeout(5 * time.Second)
	go func() {
		<-ctx.Done()
		srv.Shutdown()
	}()
	slog.Info("tftp listening", "addr", s.addr)
	return srv.ListenAndServe(s.addr)
}
