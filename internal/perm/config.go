package perm

import (
	"errors"
	"strconv"
	"strings"
)

// Action defines what to do when a ruleset matches a target address.
//
// The zero value is ActionDeny.
type Action int

// we really should keep the more severe actions numeric less

const (
	ActionDeny   Action = iota // Returns 502 Bad Gateway, and then logs the offending access.
	ActionIgnore               // Returns 502 Bad Gateway, and then don't log.
	ActionAccept               // Connects as usual.
)

// MostSevere returns the most severe of the two actions.
// E.g., ActionIgnore and ActionAccept will return ActionIgnore.
//
// This is the default behavior when the same address is matched multiple times.
func MostSevere(a, b Action) Action {
	return min(a, b)
}

func (a Action) String() (name string) {
	switch a {
	case ActionDeny:
		name = "deny"
	case ActionIgnore:
		name = "ignore"
	case ActionAccept:
		name = "accept"
	default:
		name = "<" + strconv.Itoa(int(a)) + ">"
	}
	return
}

// Marshal/Unmarshal for Action
func (a *Action) UnmarshalText(text []byte) error {
	switch strings.ToLower(string(text)) {
	case "deny":
		*a = ActionDeny
	case "ignore":
		*a = ActionIgnore
	case "accept":
		*a = ActionAccept
	default:
		return errors.New("unknown action: " + string(text))
	}
	return nil
}
func (a Action) MarshalText() ([]byte, error) {
	return []byte(a.String()), nil
}

// Config is a list of address and actions, for each source address.
// It can just be Marshal/Unmarshaled into/from json.
type Config struct {
	DefaultAction Action // What we should do when no action is matched.
	DefaultPort   []int  // Port numbers to add to address without port numbers already in them. Don't put too many entries in here.

	// Object which holds addresses and optionally ports, mapping to actions.
	//
	// Port number is extracted by net/url.splitHostPort, copied below.
	// Port number is optional, but must be numeric when present.
	Match map[string]Action
}

// validOptionalPort reports whether port is either an empty string
// or matches /^:\d*$/
func validOptionalPort(port string) bool {
	if port == "" {
		return true
	}
	if port[0] != ':' {
		return false
	}
	for _, b := range port[1:] {
		if b < '0' || b > '9' {
			return false
		}
	}
	return true
}

// splitHostPort separates host and port. If the port is not valid, it returns
// the entire input as host, and it doesn't check the validity of the host.
// Unlike net.SplitHostPort, but per RFC 3986, it requires ports to be numeric.
func splitHostPort(hostPort string) (host, port string) {
	host = hostPort

	colon := strings.LastIndexByte(host, ':')
	if colon != -1 && validOptionalPort(host[colon:]) {
		host, port = host[:colon], host[colon+1:]
	}

	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = host[1 : len(host)-1]
	}

	return
}
