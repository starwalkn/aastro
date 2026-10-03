//go:build integration

// Package integration runs the gateway in-process against stub upstreams and
// exercises it over real HTTP. Run with:
//
//	go test -tags integration ./integration/...
package integration

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"text/template"
	"time"

	"go.uber.org/zap"

	"github.com/starwalkn/aastro"
	"github.com/starwalkn/aastro/internal/server"
	"github.com/starwalkn/aastro/internal/testutil/certgen"
)

const (
	startupTimeout = 10 * time.Second
	clientTimeout  = 10 * time.Second
	logTailLines   = 200
)

// env is the shared test environment, built once in TestMain.
var env *environment

type environment struct {
	// base is the plain-HTTP gateway (testdata/gateway.yaml.tmpl)
	base   string
	client *http.Client

	tlsBase     string
	tlsPlainURL string // same port, plain http://
	mtlsClient  *http.Client
	noCertTLS   *http.Client
	rogueTLS    *http.Client

	upstreams *upstreams
	gateways  []*gateway
	dir       string
	logPath   string
}

type gatewayParams struct {
	Port, AdminPort               int
	Profile, Stats, Prefs, Stream string
	TLSDir                        string
}

func TestMain(m *testing.M) {
	os.Exit(runMain(m))
}

func runMain(m *testing.M) int {
	e, err := setup()
	if e != nil {
		defer e.teardown()
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "integration setup: %v\n", err)
		e.dumpLog()

		return 1
	}

	env = e

	code := m.Run()
	if code != 0 {
		e.dumpLog()
	}

	return code
}

func setup() (*environment, error) {
	dir, err := os.MkdirTemp("", "aastro-integration-")
	if err != nil {
		return nil, err
	}

	e := &environment{
		dir:       dir,
		logPath:   filepath.Join(dir, "gateway.log"),
		upstreams: startUpstreams(),
		client:    newClient(nil),
	}

	certs := writeTLSFixtures(dir)
	e.mtlsClient = newClient(&tls.Config{RootCAs: certs.roots, Certificates: []tls.Certificate{certs.client}})
	e.noCertTLS = newClient(&tls.Config{RootCAs: certs.roots})
	e.rogueTLS = newClient(&tls.Config{RootCAs: certs.roots, Certificates: []tls.Certificate{certs.rogue}})

	log, err := newFileLogger(e.logPath)
	if err != nil {
		return e, err
	}

	port, err := e.start("gateway.yaml.tmpl", log)
	if err != nil {
		return e, err
	}

	e.base = "http://localhost:" + strconv.Itoa(port)

	port, err = e.start("gateway-tls.yaml.tmpl", log)
	if err != nil {
		return e, err
	}

	e.tlsBase = "https://localhost:" + strconv.Itoa(port)
	e.tlsPlainURL = "http://localhost:" + strconv.Itoa(port)

	return e, nil
}

// start renders a testdata config template with free ports and the stub
// upstream addresses, then starts a gateway from it. Returns the data port.
func (e *environment) start(tmplName string, log *zap.Logger) (int, error) {
	port, err := freePort()
	if err != nil {
		return 0, err
	}

	adminPort, err := freePort()
	if err != nil {
		return 0, err
	}

	cfgPath := filepath.Join(e.dir, strings.TrimSuffix(tmplName, ".tmpl"))
	if err = renderConfig(tmplName, cfgPath, gatewayParams{
		Port:      port,
		AdminPort: adminPort,
		Profile:   e.upstreams.profile.URL,
		Stats:     e.upstreams.stats.URL,
		Prefs:     e.upstreams.prefs.URL,
		Stream:    e.upstreams.stream.URL,
		TLSDir:    e.dir,
	}); err != nil {
		return 0, err
	}

	g, err := startGateway(cfgPath, port, adminPort, log.Named(tmplName))
	if err != nil {
		return 0, fmt.Errorf("%s: %w", tmplName, err)
	}

	e.gateways = append(e.gateways, g)

	return port, nil
}

func (e *environment) teardown() {
	for _, g := range e.gateways {
		g.stop()
	}

	e.upstreams.close()
	_ = os.RemoveAll(e.dir)
}

// dumpLog prints the tail of the gateway log, so a CI failure carries the
// gateway's side of the story without having to keep artifacts around.
func (e *environment) dumpLog() {
	if e == nil {
		return
	}

	f, err := os.Open(e.logPath)
	if err != nil {
		return
	}
	defer f.Close()

	var lines []string

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		lines = append(lines, sc.Text())
		if len(lines) > logTailLines {
			lines = lines[1:]
		}
	}

	fmt.Fprintf(os.Stderr, "\n--- gateway log (last %d lines) ---\n", len(lines))

	for _, l := range lines {
		fmt.Fprintln(os.Stderr, l)
	}
}

// gateway is one in-process aastro server, started the same way
// cmd/aastro does it: LoadConfig -> server.New -> Start.
type gateway struct {
	srv  *server.Server
	done chan error
}

func startGateway(cfgPath string, port, adminPort int, log *zap.Logger) (*gateway, error) {
	cfg, err := aastro.LoadConfig(cfgPath)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), startupTimeout)
	defer cancel()

	srv, err := server.New(ctx, cfg.Gateway, "integration", log)
	if err != nil {
		return nil, err
	}

	g := &gateway{srv: srv, done: make(chan error, 1)}

	go func() { g.done <- srv.Start() }()

	if err = waitReady(ctx, port, adminPort, g.done); err != nil {
		g.stop()
		return nil, err
	}

	return g, nil
}

func (g *gateway) stop() {
	ctx, cancel := context.WithTimeout(context.Background(), startupTimeout)
	defer cancel()

	_ = g.srv.Stop(ctx)

	select {
	case <-g.done:
	case <-ctx.Done():
	}
}

// waitReady blocks until the admin /__ready endpoint answers 200 and the
// data port accepts connections, or the server exits early (e.g. the port
// grabbed by freePort was taken in the meantime).
func waitReady(ctx context.Context, port, adminPort int, done <-chan error) error {
	readyURL := fmt.Sprintf("http://127.0.0.1:%d/__ready", adminPort)
	dataAddr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))

	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()

	for {
		select {
		case err := <-done:
			return fmt.Errorf("gateway exited during startup: %w", err)
		case <-ctx.Done():
			return errors.New("gateway did not become ready in time")
		case <-tick.C:
		}

		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, readyURL, nil)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			continue
		}

		_ = resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			continue
		}

		var d net.Dialer

		if conn, dialErr := d.DialContext(ctx, "tcp", dataAddr); dialErr == nil {
			_ = conn.Close()
			return nil
		}
	}
}

// freePort asks the OS for an unused port. The server binds it again
// moments later; the gap is a theoretical race, and waitReady reports it
// as a startup error rather than a hang if it ever happens.
func freePort() (int, error) {
	var lc net.ListenConfig

	ln, err := lc.Listen(context.Background(), "tcp", ":0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()

	return ln.Addr().(*net.TCPAddr).Port, nil
}

func renderConfig(tmplName, outPath string, params gatewayParams) error {
	tmpl, err := template.ParseFiles(filepath.Join("testdata", tmplName))
	if err != nil {
		return err
	}

	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer f.Close()

	return tmpl.Execute(f, params)
}

func newFileLogger(path string) (*zap.Logger, error) {
	cfg := zap.NewProductionConfig()
	cfg.OutputPaths = []string{path}
	cfg.ErrorOutputPaths = []string{path}
	cfg.Sampling = nil

	return cfg.Build()
}

// newClient returns a client that never follows redirects, so redirect
// tests see the gateway's own response. tlsCfg is nil for plain HTTP.
func newClient(tlsCfg *tls.Config) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = tlsCfg

	return &http.Client{
		Timeout:   clientTimeout,
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

type tlsFixtures struct {
	roots         *x509.CertPool
	client, rogue tls.Certificate
}

func writeTLSFixtures(dir string) tlsFixtures {
	ca, caKey, caPEM := certgen.NewCA()
	serverCert, serverKey := certgen.NewLeaf(ca, caKey, 2)
	clientCert, clientKey := certgen.NewLeaf(ca, caKey, 3)

	rogueCA, rogueCAKey, _ := certgen.NewCA()
	rogueCert, rogueKey := certgen.NewLeaf(rogueCA, rogueCAKey, 2)

	certgen.WriteAtomic(filepath.Join(dir, "ca.crt"), caPEM)
	certgen.WriteAtomic(filepath.Join(dir, "server.crt"), serverCert)
	certgen.WriteAtomic(filepath.Join(dir, "server.key"), serverKey)

	roots := x509.NewCertPool()
	roots.AddCert(ca)

	return tlsFixtures{
		roots:  roots,
		client: certgen.KeyPair(clientCert, clientKey),
		rogue:  certgen.KeyPair(rogueCert, rogueKey),
	}
}
