package perm

import (
	"net"
	"strconv"
	"sync"
)

// Perm matches address:port strings to Actions
// loaded from a Config.
//
// It's thread safe.
type Perm struct {
	lock sync.RWMutex

	perm map[string]Action
	def  Action
}

// New creates a new Perm struct from a Config.
//
// You can also &perm.Perm{} and then call Load on it.
func New(c *Config) (p *Perm) {
	p = &Perm{}
	p.Load(c)
	return
}

// Load loads/reloads the Perm struct.
func (p *Perm) Load(c *Config) {
	p.lock.Lock()
	defer p.lock.Unlock()

	if p.perm == nil {
		p.perm = make(map[string]Action)
	} else {
		clear(p.perm)
	}

	p.def = c.DefaultAction

	// insert helper to use the most severe action existing
	insert := func(addrport string, action Action) {
		existing_action, ok := p.perm[addrport]
		if ok {
			p.perm[addrport] = MostSevere(existing_action, action)
		} else {
			p.perm[addrport] = action
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
			for def_port := range c.DefaultPort {
				insert(net.JoinHostPort(addr, strconv.Itoa(def_port)), act)
			}
		}
	}

	return
}

// Match matches an address to an action.
// addr must be in net.JoinHostPort format.
func (p *Perm) Match(addr string) Action {
	// sanity check
	if p == nil {
		return ActionDeny
	}

	p.lock.RLock()
	defer p.lock.RUnlock()

	// sanity check no.2
	if p.perm == nil {
		return p.def
	}

	action, ok := p.perm[addr]
	if !ok {
		return p.def
	}
	return action
}
