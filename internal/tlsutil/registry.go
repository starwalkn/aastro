package tlsutil

import "sync"

type Registry struct {
	mu    sync.RWMutex
	byDir map[string][]*Reloader
}

func NewRegistry() *Registry { return &Registry{byDir: map[string][]*Reloader{}} }

func (reg *Registry) Register(r *Reloader) {
	reg.mu.Lock()
	defer reg.mu.Unlock()

	for _, d := range r.WatchDirs() {
		reg.byDir[d] = append(reg.byDir[d], r) // дедуп директорий by construction
	}
}

func (reg *Registry) Dirs() []string {
	reg.mu.RLock()
	defer reg.mu.RUnlock()

	dirs := make([]string, 0, len(reg.byDir))
	for d := range reg.byDir {
		dirs = append(dirs, d)
	}

	return dirs
}

func (reg *Registry) ReloadDir(dir string) []error {
	reg.mu.RLock()
	rls := reg.byDir[dir]
	reg.mu.RUnlock()

	var errs []error

	for _, r := range rls {
		if err := r.Load(); err != nil {
			errs = append(errs, err)
		}
	}

	return errs
}
