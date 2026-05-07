package http

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"net"
	"path/filepath"
	"strings"
	"time"

	"net/http/pprof"
	"net/netip"

	"github.com/mpolden/echoip/iputil"
	"github.com/mpolden/echoip/iputil/geo"
	"github.com/mpolden/echoip/useragent"

	"math/big"
	"net/http"
	"strconv"
)

const (
	jsonMediaType = "application/json"
	textMediaType = "text/plain"
)

type Server struct {
	Template   string
	IPHeaders  []string
	LookupAddr func(netip.Addr) (string, error)
	LookupPort func(netip.Addr, uint64) error
	cache      *Cache
	gr         geo.Reader
	Sponsor    bool
	tmpl       *template.Template
}

type Response struct {
	IP         netip.Addr           `json:"ip"`
	IPDecimal  *big.Int             `json:"ip_decimal"`
	Country    string               `json:"country,omitempty"`
	CountryISO string               `json:"country_iso,omitempty"`
	CountryEU  *bool                `json:"country_eu,omitempty"`
	RegionName string               `json:"region_name,omitempty"`
	RegionCode string               `json:"region_code,omitempty"`
	PostalCode string               `json:"zip_code,omitempty"`
	City       string               `json:"city,omitempty"`
	Latitude   float64              `json:"latitude,omitempty"`
	Longitude  float64              `json:"longitude,omitempty"`
	Timezone   string               `json:"time_zone,omitempty"`
	ASN        string               `json:"asn,omitempty"`
	ASNOrg     string               `json:"asn_org,omitempty"`
	Hostname   string               `json:"hostname,omitempty"`
	UserAgent  *useragent.UserAgent `json:"user_agent,omitempty"`
}

type PortResponse struct {
	IP        netip.Addr `json:"ip"`
	Port      uint64     `json:"port"`
	Reachable bool       `json:"reachable"`
}

func New(db geo.Reader, cache *Cache) *Server {
	return &Server{cache: cache, gr: db}
}

// LoadTemplates parses all templates under s.Template once and caches the
// resulting *template.Template on the server. Callers should invoke this
// after setting Template and before serving requests, to avoid re-parsing
// the template directory on every browser hit.
func (s *Server) LoadTemplates() error {
	if s.Template == "" {
		return fmt.Errorf("template directory is not set")
	}
	t, err := template.ParseGlob(s.Template + "/*")
	if err != nil {
		return fmt.Errorf("parsing templates in %s: %w", s.Template, err)
	}
	s.tmpl = t
	return nil
}

func ipFromForwardedForHeader(v string) string {
	sep := strings.Index(v, ",")
	if sep == -1 {
		return v
	}
	return v[:sep]
}

// ipFromRequest detects the IP address for this transaction.
//
// * `headers` - the specific HTTP headers to trust
// * `r` - the incoming HTTP request
// * `customIP` - whether to allow the IP to be pulled from query parameters
func ipFromRequest(headers []string, r *http.Request, customIP bool) (netip.Addr, error) {
	remoteIP := ""
	if customIP && r.URL != nil {
		if v, ok := r.URL.Query()["ip"]; ok {
			remoteIP = v[0]
		}
	}
	if remoteIP == "" {
		for _, header := range headers {
			remoteIP = r.Header.Get(header)
			if http.CanonicalHeaderKey(header) == "X-Forwarded-For" {
				remoteIP = ipFromForwardedForHeader(remoteIP)
			}
			if remoteIP != "" {
				break
			}
		}
	}
	if remoteIP == "" {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			return netip.Addr{}, err
		}
		remoteIP = host
	}
	ip, err := netip.ParseAddr(remoteIP)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("could not parse IP: %s", remoteIP)
	}
	return ip, nil
}

func userAgentFromRequest(r *http.Request) *useragent.UserAgent {
	var userAgent *useragent.UserAgent
	userAgentRaw := r.UserAgent()
	if userAgentRaw != "" {
		parsed := useragent.Parse(userAgentRaw)
		userAgent = &parsed
	}
	return userAgent
}

func (s *Server) newResponse(r *http.Request) (Response, error) {
	ip, err := ipFromRequest(s.IPHeaders, r, true)
	if err != nil {
		return Response{}, err
	}
	response, ok := s.cache.Get(ip)
	if ok {
		// Do not cache user agent
		response.UserAgent = userAgentFromRequest(r)
		return response, nil
	}
	ipDecimal := iputil.ToDecimal(ip)
	country, _ := s.gr.Country(ip)
	city, _ := s.gr.City(ip)
	asn, _ := s.gr.ASN(ip)
	var hostname string
	if s.LookupAddr != nil {
		hostname, _ = s.LookupAddr(ip)
	}
	var autonomousSystemNumber string
	if asn.AutonomousSystemNumber > 0 {
		autonomousSystemNumber = fmt.Sprintf("AS%d", asn.AutonomousSystemNumber)
	}
	response = Response{
		IP:         ip,
		IPDecimal:  ipDecimal,
		Country:    country.Name,
		CountryISO: country.ISO,
		CountryEU:  country.IsEU,
		RegionName: city.RegionName,
		RegionCode: city.RegionCode,
		PostalCode: city.PostalCode,
		City:       city.Name,
		Latitude:   city.Latitude,
		Longitude:  city.Longitude,
		Timezone:   city.Timezone,
		ASN:        autonomousSystemNumber,
		ASNOrg:     asn.AutonomousSystemOrganization,
		Hostname:   hostname,
	}
	s.cache.Set(ip, response)
	response.UserAgent = userAgentFromRequest(r)
	return response, nil
}

func (s *Server) newPortResponse(r *http.Request) (PortResponse, error) {
	portStr := r.PathValue("port")
	if portStr == "" {
		// Fallback for callers that didn't route through the ServeMux pattern.
		portStr = filepath.Base(r.URL.Path)
	}
	port, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil || port < 1 || port > 65535 {
		return PortResponse{Port: port}, fmt.Errorf("invalid port: %s", portStr)
	}
	ip, err := ipFromRequest(s.IPHeaders, r, false)
	if err != nil {
		return PortResponse{Port: port}, err
	}
	err = s.LookupPort(ip, port)
	return PortResponse{
		IP:        ip,
		Port:      port,
		Reachable: err == nil,
	}, nil
}

func (s *Server) CLIHandler(w http.ResponseWriter, r *http.Request) *appError {
	ip, err := ipFromRequest(s.IPHeaders, r, true)
	if err != nil {
		return badRequest(err).WithMessage(err.Error()).AsJSON()
	}
	fmt.Fprintln(w, ip.String())
	return nil
}

func (s *Server) cliField(extract func(Response) string) appHandler {
	return func(w http.ResponseWriter, r *http.Request) *appError {
		resp, err := s.newResponse(r)
		if err != nil {
			return badRequest(err).WithMessage(err.Error()).AsJSON()
		}
		fmt.Fprintln(w, extract(resp))
		return nil
	}
}

func (s *Server) JSONHandler(w http.ResponseWriter, r *http.Request) *appError {
	response, err := s.newResponse(r)
	if err != nil {
		return badRequest(err).WithMessage(err.Error()).AsJSON()
	}
	b, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return internalServerError(err).AsJSON()
	}
	w.Header().Set("Content-Type", jsonMediaType)
	w.Write(b)
	return nil
}

func (s *Server) HealthHandler(w http.ResponseWriter, r *http.Request) *appError {
	w.Header().Set("Content-Type", jsonMediaType)
	w.Write([]byte(`{"status":"OK"}`))
	return nil
}

func (s *Server) PortHandler(w http.ResponseWriter, r *http.Request) *appError {
	response, err := s.newPortResponse(r)
	if err != nil {
		return badRequest(err).WithMessage(err.Error()).AsJSON()
	}
	b, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return internalServerError(err).AsJSON()
	}
	w.Header().Set("Content-Type", jsonMediaType)
	w.Write(b)
	return nil
}

func (s *Server) cacheResizeHandler(w http.ResponseWriter, r *http.Request) *appError {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return badRequest(err).WithMessage(err.Error()).AsJSON()
	}
	capacity, err := strconv.Atoi(string(body))
	if err != nil {
		return badRequest(err).WithMessage(err.Error()).AsJSON()
	}
	if err := s.cache.Resize(capacity); err != nil {
		return badRequest(err).WithMessage(err.Error()).AsJSON()
	}
	data := struct {
		Message string `json:"message"`
	}{fmt.Sprintf("Changed cache capacity to %d.", capacity)}
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return internalServerError(err).AsJSON()
	}
	w.Header().Set("Content-Type", jsonMediaType)
	w.Write(b)
	return nil
}

func (s *Server) cacheHandler(w http.ResponseWriter, r *http.Request) *appError {
	cacheStats := s.cache.Stats()
	var data = struct {
		Size      int    `json:"size"`
		Capacity  int    `json:"capacity"`
		Evictions uint64 `json:"evictions"`
	}{
		cacheStats.Size,
		cacheStats.Capacity,
		cacheStats.Evictions,
	}
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return internalServerError(err).AsJSON()
	}
	w.Header().Set("Content-Type", jsonMediaType)
	w.Write(b)
	return nil
}

func (s *Server) DefaultHandler(w http.ResponseWriter, r *http.Request) *appError {
	if s.tmpl == nil {
		return notFound(nil).WithMessage("404 page not found")
	}
	response, err := s.newResponse(r)
	if err != nil {
		return badRequest(err).WithMessage(err.Error())
	}
	json, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		return internalServerError(err)
	}

	var data = struct {
		Response
		Host         string
		BoxLatTop    float64
		BoxLatBottom float64
		BoxLonLeft   float64
		BoxLonRight  float64
		JSON         string
		Port         bool
		Sponsor      bool
	}{
		response,
		r.Host,
		response.Latitude + 0.05,
		response.Latitude - 0.05,
		response.Longitude - 0.05,
		response.Longitude + 0.05,
		string(json),
		s.LookupPort != nil,
		s.Sponsor,
	}
	if err := s.tmpl.Execute(w, &data); err != nil {
		return internalServerError(err)
	}
	return nil
}

func NotFoundHandler(w http.ResponseWriter, r *http.Request) *appError {
	err := notFound(nil).WithMessage("404 page not found")
	if r.Header.Get("accept") == jsonMediaType {
		err = err.AsJSON()
	}
	return err
}

func cliMatcher(r *http.Request) bool {
	ua := useragent.Parse(r.UserAgent())
	switch ua.Product {
	case "curl", "HTTPie", "httpie-go", "Wget", "fetch libfetch", "Go", "Go-http-client", "ddclient", "Mikrotik", "xh":
		return true
	}
	return false
}

type appHandler func(http.ResponseWriter, *http.Request) *appError

func wrapHandlerFunc(f http.HandlerFunc) appHandler {
	return func(w http.ResponseWriter, r *http.Request) *appError {
		f.ServeHTTP(w, r)
		return nil
	}
}

func (fn appHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if e := fn(w, r); e != nil { // e is *appError
		if e.Code/100 == 5 {
			log.Println(e.Error)
		}
		// When Content-Type for error is JSON, we need to marshal the response into JSON
		if e.IsJSON() {
			var data = struct {
				Code  int    `json:"status"`
				Error string `json:"error"`
			}{e.Code, e.Message}
			b, err := json.MarshalIndent(data, "", "  ")
			if err != nil {
				panic(err)
			}
			e.Message = string(b)
		}
		// Set Content-Type of response if set in error
		if e.ContentType != "" {
			w.Header().Set("Content-Type", e.ContentType)
		}
		w.WriteHeader(e.Code)
		fmt.Fprint(w, e.Message)
	}
}

// rootHandler dispatches GET / based on content negotiation, replicating the
// behavior of the previous custom router. Order matters: explicit Accept
// headers win over User-Agent sniffing, which wins over the HTML fallback.
func (s *Server) rootHandler(w http.ResponseWriter, r *http.Request) *appError {
	accept := r.Header.Get("Accept")
	switch accept {
	case jsonMediaType:
		return s.JSONHandler(w, r)
	case textMediaType:
		return s.CLIHandler(w, r)
	}
	if cliMatcher(r) {
		return s.CLIHandler(w, r)
	}
	if s.Template != "" {
		return s.DefaultHandler(w, r)
	}
	return NotFoundHandler(w, r)
}

// muxNotFoundWrapper routes unmatched requests through NotFoundHandler so 404
// responses honor content negotiation, instead of ServeMux's plain text 404.
func muxNotFoundWrapper(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, pattern := mux.Handler(r); pattern == "" {
			appHandler(NotFoundHandler).ServeHTTP(w, r)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Health
	mux.Handle("GET /health", appHandler(s.HealthHandler))

	// Root: content negotiation between JSON/CLI/HTML.
	mux.Handle("GET /{$}", appHandler(s.rootHandler))

	// JSON
	mux.Handle("GET /json", appHandler(s.JSONHandler))

	// CLI
	mux.Handle("GET /ip", appHandler(s.CLIHandler))
	if s.gr.HasCountry() {
		mux.Handle("GET /country", appHandler(s.cliField(func(r Response) string { return r.Country })))
		mux.Handle("GET /country-iso", appHandler(s.cliField(func(r Response) string { return r.CountryISO })))
	}
	if s.gr.HasCity() {
		mux.Handle("GET /city", appHandler(s.cliField(func(r Response) string { return r.City })))
		mux.Handle("GET /coordinates", appHandler(s.cliField(func(r Response) string {
			return fmt.Sprintf("%s,%s", formatCoordinate(r.Latitude), formatCoordinate(r.Longitude))
		})))
	}
	if s.gr.HasASN() {
		mux.Handle("GET /asn", appHandler(s.cliField(func(r Response) string { return r.ASN })))
		mux.Handle("GET /asn-org", appHandler(s.cliField(func(r Response) string { return r.ASNOrg })))
	}

	// Port testing
	if s.LookupPort != nil {
		mux.Handle("GET /port/{port}", appHandler(s.PortHandler))
	}

	return muxNotFoundWrapper(mux)
}

// DebugHandler returns an http.Handler exposing pprof and cache debug
// endpoints. These routes leak runtime information and include a POST endpoint
// (/debug/cache/resize) plus pprof.Profile, which can pin a CPU for the
// duration of a profile capture. The returned handler must only be served on
// a private listener (e.g. loopback) and never exposed to the public internet.
func (s *Server) DebugHandler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("POST /debug/cache/resize", appHandler(s.cacheResizeHandler))
	mux.Handle("GET /debug/cache/", appHandler(s.cacheHandler))
	mux.Handle("GET /debug/pprof/cmdline", wrapHandlerFunc(pprof.Cmdline))
	mux.Handle("GET /debug/pprof/profile", wrapHandlerFunc(pprof.Profile))
	mux.Handle("GET /debug/pprof/symbol", wrapHandlerFunc(pprof.Symbol))
	mux.Handle("GET /debug/pprof/trace", wrapHandlerFunc(pprof.Trace))
	// Trailing slash = subtree match, catches /debug/pprof/ and /debug/pprof/<profile>.
	mux.Handle("GET /debug/pprof/", wrapHandlerFunc(pprof.Index))
	return muxNotFoundWrapper(mux)
}

// newServer returns an *http.Server with conservative timeouts to mitigate
// slowloris-style resource exhaustion attacks.
func newServer(addr string, h http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}

func (s *Server) ListenAndServe(addr string) error {
	return newServer(addr, s.Handler()).ListenAndServe()
}

// ListenAndServeDebug starts an HTTP server bound to addr that serves the
// debug handler. The caller is responsible for ensuring addr is not reachable
// from untrusted networks.
func (s *Server) ListenAndServeDebug(addr string) error {
	return newServer(addr, s.DebugHandler()).ListenAndServe()
}

func formatCoordinate(c float64) string {
	return strconv.FormatFloat(c, 'f', 6, 64)
}
