package policy

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

type IdentityClass string

const (
	IdentityAuthenticated IdentityClass = "authenticated"
	IdentityAnonymous     IdentityClass = "anonymous"
)

type Rule struct {
	Name, Resource string
	Identity       IdentityClass
	Algorithm      string
	Limit          int
	Window         time.Duration
	Cost           int
	FailMode       int
}
type Resolver struct {
	mu      sync.RWMutex
	rules   map[string]Rule
	version uint64
}

func NewResolver(rules []Rule) (*Resolver, error) {
	x := &Resolver{rules: map[string]Rule{}}
	for _, r := range rules {
		if err := x.Put(r); err != nil {
			return nil, err
		}
	}
	return x, nil
}
func (r *Resolver) Put(rule Rule) error {
	if strings.TrimSpace(rule.Name) == "" || strings.TrimSpace(rule.Resource) == "" {
		return fmt.Errorf("rule name and resource are required")
	}
	if strings.TrimSpace(rule.Algorithm) == "" {
		return fmt.Errorf("rule algorithm is required")
	}
	if err := Validate(rule.Limit, rule.Window); err != nil {
		return err
	}
	if rule.Cost <= 0 {
		rule.Cost = 1
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rules[rule.Name] = rule
	r.version++
	return nil
}
func (r *Resolver) Resolve(resource string, class IdentityClass) (Rule, bool, uint64) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, rule := range r.rules {
		if rule.Resource == resource && (rule.Identity == class || rule.Identity == "") {
			return rule, true, r.version
		}
	}
	return Rule{}, false, r.version
}
func (r *Resolver) Version() uint64 { r.mu.RLock(); defer r.mu.RUnlock(); return r.version }
