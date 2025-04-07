package perm

import (
	"log"
	"net"
	"strconv"
	"strings"
	"sync"

	"edgaru089.ink/go/regolith/internal/util"
)

type int_perm struct {
	match      map[string]Action
	match_glob []globAction
	def        Action
}

// Perm matches address:port strings to Actions
// loaded from a Config.
//
// global permissions are stored under the "$global" address.
//
// It's thread safe.
type Perm struct {
	lock sync.RWMutex

	global int_perm

	source map[string]int_perm
}

// New creates a new Perm struct from a Config.
//
// You can also &perm.Perm{} and then call Load on it.
func New(c map[string]Config) (p *Perm) {
	p = &Perm{}
	p.Load(c)
	return
}

// Load loads/reloads the Perm struct.
func (p *Perm) Load(cs map[string]Config) {
	p.lock.Lock()
	defer p.lock.Unlock()

	if p.source == nil {
		p.source = make(map[string]int_perm)
	} else {
		clear(p.source)
	}

	// helper to load every source addr
	load_per_source := func(c Config) (p_int int_perm) {
		p_int.match = make(map[string]Action)
		p_int.def = c.DefaultAction
		log.Printf("default action %s", p_int.def)

		// insert helper to use the most severe action existing
		insert := func(addrport string, action Action) {
			log.Printf("loading target %s, action %s", addrport, action)
			existing_action, ok := p_int.match[addrport]
			if ok {
				p_int.match[addrport] = MostSevere(existing_action, action)
			} else {
				p_int.match[addrport] = action
			}
		}

		// loop around the Match map
		for addrport, act := range c.Match {
			addr, port := splitHostPort(addrport)
			if port != "" {
				insert(net.JoinHostPort(addr, port), act)
			} else {
				// so this is why def_port shouldn't be that big
				// TODO change this to sth faster
				for _, def_port := range c.DefaultPort {
					insert(net.JoinHostPort(addr, strconv.Itoa(def_port)), act)
				}
			}
		}
		for _, glob := range c.MatchWildcard {
			addr, port := splitHostPort(glob.Glob)
			if port != "" {
				log.Printf("loading glob target %s, action %s", glob.Glob, glob.Act)
				p_int.match_glob = append(
					p_int.match_glob,
					globAction{
						Glob: glob.Glob,
						Act:  glob.Act,
					})
			} else {
				// TODO change this to sth faster
				for _, def_port := range c.DefaultPort {
					log.Printf("loading glob target %s, action %s", net.JoinHostPort(addr, strconv.Itoa(def_port)), glob.Act)
					p_int.match_glob = append(
						p_int.match_glob,
						globAction{
							Glob: net.JoinHostPort(addr, strconv.Itoa(def_port)),
							Act:  glob.Act,
						})
				}
			}
		}
		return
	}

	for src, c := range cs {
		log.Printf("loading source %s", src)
		if strings.EqualFold(src, "$global") {
			p.global = load_per_source(c)
		} else {
			p.source[src] = load_per_source(c)
		}
	}

	return
}

// Match matches an address to an action.
//
// src must be a host (either ipv4 or v6), while
// dest must be in net.JoinHostPort format.
func (p *Perm) Match(src, dest string) Action {
	// sanity check
	if p == nil {
		return ActionDeny
	}

	p.lock.RLock()
	defer p.lock.RUnlock()

	// find its source struct
	p_int, ok_int := p.source[src]
	// only check if dest is directly listed
	if ok_int {
		// first check direct match
		if p_int.match != nil {
			if action, ok := p_int.match[dest]; ok {
				return action
			}
		}
		// then check glob match
		for _, g := range p_int.match_glob {
			if util.Match(g.Glob, dest) {
				return g.Act
			}
		}
	}

	// then check the global struct, also only directly listed
	if p.global.match != nil {
		if action, ok := p.global.match[dest]; ok {
			return action
		}
	}
	// then check global glob match
	for _, g := range p.global.match_glob {
		if util.Match(g.Glob, dest) {
			return g.Act
		}
	}

	// directly listed in neither.
	if ok_int {
		// if source struct exists, use source struct default.
		return p_int.def
	} else {
		// if not exist, use global default.
		return p.global.def
	}
}
