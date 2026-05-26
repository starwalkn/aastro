package aastro

import (
	"net/http"

	"github.com/starwalkn/aastro/sdk"
)

// aastroContext is the internal per-request context passed to plugins.
// It implements sdk.Context and is created once per request in newFlowHandler.
type aastroContext struct {
	req  *http.Request
	resp *http.Response
}

func newContext(req *http.Request) sdk.Context {
	return &aastroContext{req: req}
}

func (c *aastroContext) Request() *http.Request {
	return c.req
}

func (c *aastroContext) Response() *http.Response {
	return c.resp
}

func (c *aastroContext) SetRequest(req *http.Request) {
	c.req = req
}

func (c *aastroContext) SetResponse(resp *http.Response) {
	c.resp = resp
}
