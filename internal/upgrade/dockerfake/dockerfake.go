// Package dockerfake is a small in-memory Docker Engine API used to test the
// Docker upgrade driver. What "running a container" means is up to a Runtime:
// unit tests keep everything in memory, the integration test starts real
// AdGuard Home binaries.
package dockerfake

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path"
	"strings"
	"sync"
)

// Container is a fake container.
type Container struct {
	ID         string
	Name       string // without the leading slash
	Image      string
	Config     map[string]any
	HostConfig map[string]any
	Networks   map[string]map[string]any
	Running    bool
}

// Runtime decides what starting, stopping and file access do.
type Runtime interface {
	Start(c *Container) error
	Stop(c *Container) error
	ReadFile(c *Container, p string) ([]byte, error)
	WriteFile(c *Container, p string, data []byte) error
}

// Server is a fake Docker API.
type Server struct {
	*httptest.Server
	RT Runtime
	// Pullable reports whether repo:tag exists in the "registry".
	Pullable func(repo, tag string) bool

	mu         sync.Mutex
	containers map[string]*Container
	seq        int
	// Calls records "METHOD /path" for assertions.
	Calls []string
}

// New starts a fake Docker API on a random local port.
func New(rt Runtime) *Server {
	s := NewUnstarted(rt)
	s.Start()
	return s
}

// NewUnstarted returns a fake Docker API; set Listener, then call Start.
func NewUnstarted(rt Runtime) *Server {
	s := &Server{RT: rt, containers: map[string]*Container{}, Pullable: func(string, string) bool { return true }}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /version", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 200, map[string]string{"Version": "27.0.0-fake", "ApiVersion": "1.46"})
	})
	mux.HandleFunc("GET /containers/{id}/json", s.inspect)
	mux.HandleFunc("POST /images/create", s.pull)
	mux.HandleFunc("POST /containers/{id}/stop", s.stop)
	mux.HandleFunc("POST /containers/{id}/start", s.start)
	mux.HandleFunc("POST /containers/{id}/rename", s.rename)
	mux.HandleFunc("DELETE /containers/{id}", s.remove)
	mux.HandleFunc("POST /containers/create", s.create)
	mux.HandleFunc("POST /networks/{net}/connect", s.connect)
	mux.HandleFunc("GET /containers/{id}/archive", s.getArchive)
	mux.HandleFunc("PUT /containers/{id}/archive", s.putArchive)
	s.Server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.Calls = append(s.Calls, r.Method+" "+r.URL.Path)
		s.mu.Unlock()
		mux.ServeHTTP(w, r)
	}))
	return s
}

// Add registers an existing container.
func (s *Server) Add(c *Container) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c.ID == "" {
		s.seq++
		c.ID = fmt.Sprintf("%064x", s.seq)
	}
	if c.Config == nil {
		c.Config = map[string]any{}
	}
	c.Config["Image"] = c.Image
	if c.HostConfig == nil {
		c.HostConfig = map[string]any{"NetworkMode": "bridge"}
	}
	if c.Networks == nil {
		c.Networks = map[string]map[string]any{"bridge": {}}
	}
	s.containers[c.ID] = c
}

// Get finds a container by name or ID.
func (s *Server) Get(ref string) *Container {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.find(ref)
}

// List returns all containers.
func (s *Server) List() []*Container {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Container, 0, len(s.containers))
	for _, c := range s.containers {
		out = append(out, c)
	}
	return out
}

func (s *Server) find(ref string) *Container {
	if c, ok := s.containers[ref]; ok {
		return c
	}
	for _, c := range s.containers {
		if c.Name == strings.TrimPrefix(ref, "/") || (len(ref) >= 12 && strings.HasPrefix(c.ID, ref)) {
			return c
		}
	}
	return nil
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func msg(w http.ResponseWriter, code int, m string) {
	writeJSON(w, code, map[string]string{"message": m})
}

func (s *Server) lookup(w http.ResponseWriter, r *http.Request) *Container {
	s.mu.Lock()
	c := s.find(r.PathValue("id"))
	s.mu.Unlock()
	if c == nil {
		msg(w, 404, "No such container: "+r.PathValue("id"))
	}
	return c
}

func (s *Server) inspect(w http.ResponseWriter, r *http.Request) {
	c := s.lookup(w, r)
	if c == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	writeJSON(w, 200, map[string]any{
		"Id": c.ID, "Name": "/" + c.Name, "Image": "sha256:" + c.Image, "Config": c.Config, "HostConfig": c.HostConfig,
		"NetworkSettings": map[string]any{"Networks": c.Networks},
		"State":           map[string]any{"Running": c.Running, "Status": map[bool]string{true: "running", false: "exited"}[c.Running]},
	})
}

func (s *Server) pull(w http.ResponseWriter, r *http.Request) {
	repo, tag := r.URL.Query().Get("fromImage"), r.URL.Query().Get("tag")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	if !s.Pullable(repo, tag) {
		_, _ = fmt.Fprintf(w, "{\"status\":\"Pulling from %s\"}\n{\"error\":\"manifest for %s:%s not found\"}\n", repo, repo, tag)
		return
	}
	_, _ = fmt.Fprintf(w, "{\"status\":\"Pulling from %s\"}\n{\"status\":\"Status: Downloaded newer image for %s:%s\"}\n", repo, repo, tag)
}

func (s *Server) stop(w http.ResponseWriter, r *http.Request) {
	c := s.lookup(w, r)
	if c == nil {
		return
	}
	if !c.Running {
		w.WriteHeader(304)
		return
	}
	if err := s.RT.Stop(c); err != nil {
		msg(w, 500, err.Error())
		return
	}
	s.mu.Lock()
	c.Running = false
	s.mu.Unlock()
	w.WriteHeader(204)
}

func (s *Server) start(w http.ResponseWriter, r *http.Request) {
	c := s.lookup(w, r)
	if c == nil {
		return
	}
	if c.Running {
		w.WriteHeader(304)
		return
	}
	if err := s.RT.Start(c); err != nil {
		msg(w, 500, err.Error())
		return
	}
	s.mu.Lock()
	c.Running = true
	s.mu.Unlock()
	w.WriteHeader(204)
}

func (s *Server) rename(w http.ResponseWriter, r *http.Request) {
	c := s.lookup(w, r)
	if c == nil {
		return
	}
	name := r.URL.Query().Get("name")
	s.mu.Lock()
	defer s.mu.Unlock()
	if o := s.find(name); o != nil && o != c {
		msg(w, 409, "name "+name+" is already in use")
		return
	}
	c.Name = name
	w.WriteHeader(204)
}

func (s *Server) remove(w http.ResponseWriter, r *http.Request) {
	c := s.lookup(w, r)
	if c == nil {
		return
	}
	if c.Running {
		_ = s.RT.Stop(c)
	}
	s.mu.Lock()
	delete(s.containers, c.ID)
	s.mu.Unlock()
	w.WriteHeader(204)
}

func (s *Server) create(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		msg(w, 400, err.Error())
		return
	}
	s.mu.Lock()
	if s.find(name) != nil {
		s.mu.Unlock()
		msg(w, 409, "Conflict. The container name \"/"+name+"\" is already in use")
		return
	}
	s.mu.Unlock()
	hc, _ := body["HostConfig"].(map[string]any)
	delete(body, "HostConfig")
	nets := map[string]map[string]any{}
	if nc, ok := body["NetworkingConfig"].(map[string]any); ok {
		if eps, ok := nc["EndpointsConfig"].(map[string]any); ok {
			for k, v := range eps {
				m, _ := v.(map[string]any)
				nets[k] = m
			}
		}
	}
	delete(body, "NetworkingConfig")
	img, _ := body["Image"].(string)
	c := &Container{Name: name, Image: img, Config: body, HostConfig: hc, Networks: nets}
	s.Add(c)
	writeJSON(w, 201, map[string]any{"Id": c.ID, "Warnings": []string{}})
}

func (s *Server) connect(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Container      string         `json:"Container"`
		EndpointConfig map[string]any `json:"EndpointConfig"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		msg(w, 400, err.Error())
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.find(body.Container)
	if c == nil {
		msg(w, 404, "no such container")
		return
	}
	c.Networks[r.PathValue("net")] = body.EndpointConfig
	w.WriteHeader(200)
}

func (s *Server) getArchive(w http.ResponseWriter, r *http.Request) {
	c := s.lookup(w, r)
	if c == nil {
		return
	}
	p := r.URL.Query().Get("path")
	data, err := s.RT.ReadFile(c, p)
	if err != nil {
		msg(w, 404, "Could not find the file "+p+" in container "+c.Name)
		return
	}
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	_ = tw.WriteHeader(&tar.Header{Name: path.Base(p), Mode: 0o600, Size: int64(len(data)), Typeflag: tar.TypeReg})
	_, _ = tw.Write(data)
	_ = tw.Close()
	w.Header().Set("Content-Type", "application/x-tar")
	_, _ = w.Write(buf.Bytes())
}

func (s *Server) putArchive(w http.ResponseWriter, r *http.Request) {
	c := s.lookup(w, r)
	if c == nil {
		return
	}
	dir := r.URL.Query().Get("path")
	tr := tar.NewReader(r.Body)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			msg(w, 400, err.Error())
			return
		}
		data, _ := io.ReadAll(tr)
		if err := s.RT.WriteFile(c, path.Join(dir, h.Name), data); err != nil {
			msg(w, 500, err.Error())
			return
		}
	}
	w.WriteHeader(200)
}

// MemRuntime keeps files in memory and fails to start images in Broken.
type MemRuntime struct {
	mu     sync.Mutex
	Files  map[string][]byte // shared volume: path -> content
	Broken map[string]bool   // image refs that fail to start
	Starts []string          // image refs started, in order
}

// Start implements Runtime.
func (m *MemRuntime) Start(c *Container) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Broken[c.Image] {
		return fmt.Errorf("container %s exited immediately (exit code 1)", c.Name)
	}
	m.Starts = append(m.Starts, c.Image)
	return nil
}

// Stop implements Runtime.
func (m *MemRuntime) Stop(*Container) error { return nil }

// ReadFile implements Runtime.
func (m *MemRuntime) ReadFile(_ *Container, p string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.Files[p]
	if !ok {
		return nil, fmt.Errorf("no such file %s", p)
	}
	return d, nil
}

// WriteFile implements Runtime.
func (m *MemRuntime) WriteFile(_ *Container, p string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Files == nil {
		m.Files = map[string][]byte{}
	}
	m.Files[p] = data
	return nil
}
