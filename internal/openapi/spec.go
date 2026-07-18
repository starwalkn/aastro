// Package openapi generates OpenAPI 3.x documents from an aastro gateway
// configuration. The package models only the subset of the specification
// that the generator emits and has no dependencies outside the standard
// library — serialization (YAML/JSON) is left to the caller (aastroctl).
package openapi

// Document is the root OpenAPI object.
// Field order matches the conventional layout of a hand-written spec.
type Document struct {
	OpenAPI    string               `yaml:"openapi"              json:"openapi"`
	Info       Info                 `yaml:"info"                 json:"info"`
	Servers    []Server             `yaml:"servers,omitempty"    json:"servers,omitempty"`
	Tags       []Tag                `yaml:"tags,omitempty"       json:"tags,omitempty"`
	Paths      map[string]*PathItem `yaml:"paths"                json:"paths"`
	Components *Components          `yaml:"components,omitempty" json:"components,omitempty"`
	XAastro    *RootExtension       `yaml:"x-aastro,omitempty"   json:"x-aastro,omitempty"`
}

type Info struct {
	Title       string `yaml:"title"                 json:"title"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	Version     string `yaml:"version"               json:"version"`
}

type Server struct {
	URL string `yaml:"url" json:"url"`
}

type Tag struct {
	Name string `yaml:"name" json:"name"`
}

// PathItem holds one operation per HTTP method.
// QUERY is not part of the OAS 3.x fixed method set, so flows using it are
// emitted under the x-aastro-query extension instead of being dropped.
type PathItem struct {
	Get     *Operation `yaml:"get,omitempty"     json:"get,omitempty"`
	Put     *Operation `yaml:"put,omitempty"     json:"put,omitempty"`
	Post    *Operation `yaml:"post,omitempty"    json:"post,omitempty"`
	Delete  *Operation `yaml:"delete,omitempty"  json:"delete,omitempty"`
	Options *Operation `yaml:"options,omitempty" json:"options,omitempty"`
	Head    *Operation `yaml:"head,omitempty"    json:"head,omitempty"`
	Patch   *Operation `yaml:"patch,omitempty"   json:"patch,omitempty"`

	XAastroQuery *Operation `yaml:"x-aastro-query,omitempty" json:"x-aastro-query,omitempty"`
}

type Operation struct {
	Tags        []string              `yaml:"tags,omitempty"        json:"tags,omitempty"`
	Summary     string                `yaml:"summary,omitempty"     json:"summary,omitempty"`
	Description string                `yaml:"description,omitempty" json:"description,omitempty"`
	OperationID string                `yaml:"operationId,omitempty" json:"operationId,omitempty"`
	Parameters  []Parameter           `yaml:"parameters,omitempty"  json:"parameters,omitempty"`
	RequestBody *RequestBody          `yaml:"requestBody,omitempty" json:"requestBody,omitempty"`
	Security    []map[string][]string `yaml:"security,omitempty"   json:"security,omitempty"`
	Responses   map[string]*Response  `yaml:"responses"             json:"responses"`
	XAastro     *FlowExtension        `yaml:"x-aastro,omitempty"    json:"x-aastro,omitempty"`
}

type Parameter struct {
	Name        string  `yaml:"name"                  json:"name"`
	In          string  `yaml:"in"                    json:"in"` // path | query | header
	Description string  `yaml:"description,omitempty" json:"description,omitempty"`
	Required    bool    `yaml:"required,omitempty"    json:"required,omitempty"`
	Schema      *Schema `yaml:"schema,omitempty"      json:"schema,omitempty"`
}

type RequestBody struct {
	Description string               `yaml:"description,omitempty" json:"description,omitempty"`
	Required    bool                 `yaml:"required,omitempty"    json:"required,omitempty"`
	Content     map[string]MediaType `yaml:"content"               json:"content"`
}

type MediaType struct {
	Schema *Schema `yaml:"schema,omitempty" json:"schema,omitempty"`
}

type Response struct {
	Description string               `yaml:"description"       json:"description"`
	Headers     map[string]*Header   `yaml:"headers,omitempty" json:"headers,omitempty"`
	Content     map[string]MediaType `yaml:"content,omitempty" json:"content,omitempty"`
}

type Header struct {
	Description string  `yaml:"description,omitempty" json:"description,omitempty"`
	Schema      *Schema `yaml:"schema,omitempty"      json:"schema,omitempty"`
}

// Schema is a minimal JSON Schema node. The generator only emits shapes valid
// in both OAS 3.0 and 3.1 (no nullable, no type arrays), which is what keeps
// dual-version support a one-line difference.
type Schema struct {
	Ref         string             `yaml:"$ref,omitempty"        json:"$ref,omitempty"`
	Type        string             `yaml:"type,omitempty"        json:"type,omitempty"`
	Description string             `yaml:"description,omitempty" json:"description,omitempty"`
	Enum        []string           `yaml:"enum,omitempty"        json:"enum,omitempty"`
	Items       *Schema            `yaml:"items,omitempty"       json:"items,omitempty"`
	Properties  map[string]*Schema `yaml:"properties,omitempty"  json:"properties,omitempty"`
}

type Components struct {
	Schemas         map[string]*Schema         `yaml:"schemas,omitempty"         json:"schemas,omitempty"`
	SecuritySchemes map[string]*SecurityScheme `yaml:"securitySchemes,omitempty" json:"securitySchemes,omitempty"`
}

type SecurityScheme struct {
	Type         string `yaml:"type"                   json:"type"`
	Scheme       string `yaml:"scheme,omitempty"       json:"scheme,omitempty"`
	BearerFormat string `yaml:"bearerFormat,omitempty" json:"bearerFormat,omitempty"`
	Description  string `yaml:"description,omitempty"  json:"description,omitempty"`
}

// RootExtension identifies the generator so a future `aastroctl openapi import`
// can recognize its own output and enable lossless round-trip.
type RootExtension struct {
	Schema    string `yaml:"schema"    json:"schema"` // aastro config schema version, e.g. "v1"
	Generator string `yaml:"generator" json:"generator"`
}

// FlowExtension is a lossless snapshot of the flow attached to each operation
// when Options.Extensions is enabled. TLS material and plugin/middleware
// paths are intentionally excluded: specs frequently leave the perimeter.
type FlowExtension struct {
	Passthrough bool                  `yaml:"passthrough,omitempty" json:"passthrough,omitempty"`
	Aggregation *AggregationExtension `yaml:"aggregation,omitempty" json:"aggregation,omitempty"`
	Upstreams   []UpstreamExtension   `yaml:"upstreams"             json:"upstreams"`
	// Middlewares carries names only: middleware configs may contain secrets
	// (hmac_secret and the like) and never belong in an exported spec.
	Middlewares []string `yaml:"middlewares,omitempty" json:"middlewares,omitempty"`
}

type AggregationExtension struct {
	Strategy   string               `yaml:"strategy"              json:"strategy"`
	BestEffort bool                 `yaml:"best_effort,omitempty" json:"best_effort,omitempty"`
	OnConflict *OnConflictExtension `yaml:"on_conflict,omitempty" json:"on_conflict,omitempty"`
}

type OnConflictExtension struct {
	Policy         string `yaml:"policy"                    json:"policy"`
	PreferUpstream string `yaml:"prefer_upstream,omitempty" json:"prefer_upstream,omitempty"`
}

type UpstreamExtension struct {
	Name           string   `yaml:"name"                      json:"name"`
	Hosts          []string `yaml:"hosts"                     json:"hosts"`
	Path           string   `yaml:"path,omitempty"            json:"path,omitempty"`
	Method         string   `yaml:"method,omitempty"          json:"method,omitempty"`
	Timeout        string   `yaml:"timeout,omitempty"         json:"timeout,omitempty"`
	ForwardHeaders []string `yaml:"forward_headers,omitempty" json:"forward_headers,omitempty"`
	ForwardQueries []string `yaml:"forward_queries,omitempty" json:"forward_queries,omitempty"`
	ForwardParams  []string `yaml:"forward_params,omitempty"  json:"forward_params,omitempty"`
}
