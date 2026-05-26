package aastro

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/zap"
	"golang.org/x/sync/semaphore"

	"github.com/starwalkn/aastro/internal/circuitbreaker"
	"github.com/starwalkn/aastro/sdk"
)

// ── Config helpers ────────────────────────────────────────────────────────────

func testUpstreamConfig(port string) UpstreamConfig {
	return UpstreamConfig{
		Name:    "test_service_" + port,
		Hosts:   AddrList{"test-service:" + port},
		Path:    "/builder/test",
		Method:  http.MethodGet,
		Timeout: 5 * time.Second,
	}
}

// ── Upstream stubs ────────────────────────────────────────────────────────────

type mockUpstream struct {
	upstreamName string
}

func (s *mockUpstream) name() string { return s.upstreamName }
func (s *mockUpstream) call(_ context.Context, _ *http.Request, _ []byte) *upstreamResponse {
	return &upstreamResponse{}
}

type mockProxyUpstream struct {
	upstreamName string
	proxyFn      func(w http.ResponseWriter, req *http.Request) error
}

func (m *mockProxyUpstream) name() string { return m.upstreamName }
func (m *mockProxyUpstream) call(_ context.Context, _ *http.Request, _ []byte) *upstreamResponse {
	return &upstreamResponse{}
}

func (m *mockProxyUpstream) proxy(_ context.Context, w http.ResponseWriter, req *http.Request) error {
	return m.proxyFn(w, req)
}

func mockUpstreams(names ...string) []upstream {
	upstreams := make([]upstream, len(names))

	for i, name := range names {
		upstreams[i] = &mockUpstream{upstreamName: name}
	}

	return upstreams
}

// ── Plugin and middleware mocks ───────────────────────────────────────────────

type mockPlugin struct {
	name string
	typ  sdk.PluginType
	fn   func(sdk.Context)
}

func (m *mockPlugin) Init(_ map[string]interface{}) error { return nil }
func (m *mockPlugin) Info() sdk.PluginInfo {
	return sdk.PluginInfo{
		Name:        m.name,
		Description: "Mock plugin",
		Version:     "v1",
		Author:      "test",
	}
}
func (m *mockPlugin) Type() sdk.PluginType { return m.typ }
func (m *mockPlugin) Execute(ctx sdk.Context) error {
	m.fn(ctx)
	return nil
}

type mockMiddleware struct{}

func (m *mockMiddleware) Init(_ map[string]interface{}) error { return nil }
func (m *mockMiddleware) Name() string                        { return "mockmw" }
func (m *mockMiddleware) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("X-Middleware", "ok")
		next.ServeHTTP(w, r)
	})
}

// ── Scatter mock ──────────────────────────────────────────────────────────────

type mockScatter struct {
	results []upstreamResponse
}

func (m *mockScatter) scatter(_ *flow, _ *http.Request) []upstreamResponse {
	return m.results
}

// ── Builders ──────────────────────────────────────────────────────────────────

func newTestRouter(flows []flow, d scatter, a aggregator) *Router {
	r := &Router{
		chiRouter:  chi.NewMux(),
		scatter:    d,
		aggregator: a,
		flows:      flows,
		log:        zap.NewNop(),
		metrics:    testMetrics,
	}
	r.registerFlows()
	return r
}

func newTestUpstream(host string, opts ...func(*httpUpstream)) *httpUpstream {
	u := &httpUpstream{
		cfg: upstreamConfig{
			hosts:   []string{host},
			method:  http.MethodGet,
			timeout: 500 * time.Millisecond,
		},
		state:   upstreamState{},
		metrics: testMetrics,
		log:     zap.NewNop(),
		client:  http.DefaultClient,
	}
	for _, opt := range opts {
		opt(u)
	}
	return u
}

func withMethod(method string) func(*httpUpstream) {
	return func(u *httpUpstream) { u.cfg.method = method }
}

func withTimeout(d time.Duration) func(*httpUpstream) {
	return func(u *httpUpstream) { u.cfg.timeout = d }
}

func withPolicy(p upstreamPolicy) func(*httpUpstream) {
	return func(u *httpUpstream) { u.cfg.policy = p }
}

func withForwardQueries(queries ...string) func(*httpUpstream) {
	return func(u *httpUpstream) { u.cfg.forwardQueries = queries }
}

func withForwardHeaders(headers ...string) func(*httpUpstream) {
	return func(u *httpUpstream) { u.cfg.forwardHeaders = headers }
}

func withForwardParams(params ...string) func(*httpUpstream) {
	return func(u *httpUpstream) { u.cfg.forwardParams = params }
}

func withPath(path string) func(*httpUpstream) {
	return func(u *httpUpstream) { u.cfg.path = path }
}

func withHosts(hosts ...string) func(*httpUpstream) {
	return func(u *httpUpstream) { u.cfg.hosts = hosts }
}

func withLBMode(mode lbMode, hostCount int) func(*httpUpstream) {
	return func(u *httpUpstream) {
		u.cfg.lbMode = mode
		u.state.activeConnections = make([]int64, hostCount)
	}
}

func withCircuitBreaker(cb *circuitbreaker.CircuitBreaker) func(*httpUpstream) {
	return func(u *httpUpstream) { u.circuitBreaker = cb }
}

func withTrustedProxies(proxies ...*net.IPNet) func(*httpUpstream) {
	return func(u *httpUpstream) { u.cfg.trustedProxies = proxies }
}

func withTLS(tlsConfig *tls.Config) func(*httpUpstream) {
	return func(u *httpUpstream) {
		u.client = &http.Client{
			Transport: &http.Transport{TLSClientConfig: tlsConfig},
		}
	}
}

// ── TLS fixture ───────────────────────────────────────────────────────────────

type tlsFixture struct {
	caCert *x509.Certificate
	caKey  *ecdsa.PrivateKey
}

func newTLSFixture() *tlsFixture {
	GinkgoHelper()

	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	Expect(err).ToNot(HaveOccurred())

	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "aastro-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &caKey.PublicKey, caKey)
	Expect(err).ToNot(HaveOccurred())

	caCert, err := x509.ParseCertificate(der)
	Expect(err).ToNot(HaveOccurred())

	return &tlsFixture{caCert: caCert, caKey: caKey}
}

func (f *tlsFixture) IssueServerCert() tls.Certificate {
	GinkgoHelper()

	return f.issueCert(certParams{
		cn:          "test-server",
		eku:         []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		dnsNames:    []string{"localhost"},
		ipAddresses: []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
	})
}

func (f *tlsFixture) IssueClientCert(cn string) tls.Certificate {
	GinkgoHelper()

	return f.issueCert(certParams{
		cn:  cn,
		eku: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	})
}

func (f *tlsFixture) CertPool() *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(f.caCert)
	return pool
}

type certParams struct {
	cn          string
	eku         []x509.ExtKeyUsage
	dnsNames    []string
	ipAddresses []net.IP
}

func (f *tlsFixture) issueCert(p certParams) tls.Certificate {
	GinkgoHelper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	Expect(err).ToNot(HaveOccurred())

	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: p.cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  p.eku,
		DNSNames:     p.dnsNames,
		IPAddresses:  p.ipAddresses,
	}

	der, err := x509.CreateCertificate(rand.Reader, template, f.caCert, &key.PublicKey, f.caKey)
	Expect(err).ToNot(HaveOccurred())

	return tls.Certificate{
		Certificate: [][]byte{der},
		PrivateKey:  key,
	}
}

// ── Flow / scatter builders ───────────────────────────────────────────────────

func newTestFlow(upstreams []upstream, parallelUpstreams int64) *flow {
	return &flow{
		upstreams: upstreams,
		sem:       semaphore.NewWeighted(parallelUpstreams),
	}
}

func newTestScatter() *defaultScatter {
	return &defaultScatter{
		log:     zap.NewNop(),
		metrics: testMetrics,
	}
}

func passthroughFlow(path string, u upstream, plugins ...sdk.Plugin) flow {
	return flow{
		path:        path,
		method:      http.MethodGet,
		passthrough: true,
		upstreams:   []upstream{u},
		plugins:     plugins,
	}
}

// ── Response builders ─────────────────────────────────────────────────────────

func okResponse(body string) upstreamResponse {
	return upstreamResponse{body: []byte(body)}
}

func errResponse(kind upstreamErrorKind) upstreamResponse {
	return upstreamResponse{
		err: &upstreamError{
			kind: kind,
			err:  fmt.Errorf("upstream error: %s", kind),
		},
	}
}

// ── Request builders ──────────────────────────────────────────────────────────

func requestWithClientIP(method, url, remoteAddr, clientIP string) *http.Request {
	req, _ := http.NewRequest(method, url, nil)
	req.RemoteAddr = remoteAddr
	req = req.WithContext(withClientIP(req.Context(), clientIP))
	return req
}

func mustParseCIDR(cidr string) *net.IPNet {
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		panic(err)
	}

	return ipnet
}

// ── Assertions ────────────────────────────────────────────────────────────────

func jsonEqual(expected string, actual []byte) {
	GinkgoHelper()
	var e, a any
	Expect(json.Unmarshal([]byte(expected), &e)).To(Succeed())
	Expect(json.Unmarshal(actual, &a)).To(Succeed())
	Expect(a).To(Equal(e))
}

func decodeJSONResponse(body []byte) ClientResponse {
	GinkgoHelper()
	var resp ClientResponse
	Expect(json.Unmarshal(body, &resp)).To(Succeed())
	return resp
}

func decodeJSONInto(data []byte, dst any) error {
	return json.Unmarshal(data, dst)
}
