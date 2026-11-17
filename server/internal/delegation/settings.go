// Package delegation manages installation-owned delegation profiles.
package delegation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/storage"
)

const PolicyKey = "delegation/policy"
const ProfilePrefix = "delegation/profiles/"
const MaxSlots = 32

var ID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

type Policy struct {
	Enabled                 bool   `json:"enabled"`
	ApprovalMode            string `json:"approvalMode"`
	MaxDepth                int    `json:"maxDepth"`
	MaxChildrenPerExecution int    `json:"maxChildrenPerExecution"`
	MaxConcurrentChildren   int    `json:"maxConcurrentChildren"`
	MaxDurationSeconds      int    `json:"maxDurationSeconds"`
	MaxOutputTokens         int    `json:"maxOutputTokens"`
	MaxObjectiveBytes       int    `json:"maxObjectiveBytes"`
	MaxResultBytes          int    `json:"maxResultBytes"`
	MaxTreeTokens           int    `json:"maxTreeTokens"`
}

func DefaultPolicy() Policy {
	return Policy{true, "required", 1, 3, 4, 120, 1024, 4096, 16384, DefaultTreeTokens}
}
func (p Policy) Validate() error {
	if p.ApprovalMode != "required" && p.ApprovalMode != "automatic" {
		return errors.New("approvalMode must be required or automatic")
	}
	if p.MaxDepth < 1 || p.MaxDepth > 4 {
		return errors.New("maxDepth must be between 1 and 4")
	}
	for _, v := range []struct {
		name   string
		n, max int
	}{{"maxChildrenPerExecution", p.MaxChildrenPerExecution, 32}, {"maxConcurrentChildren", p.MaxConcurrentChildren, MaxSlots}, {"maxDurationSeconds", p.MaxDurationSeconds, 600}, {"maxOutputTokens", p.MaxOutputTokens, 4096}, {"maxObjectiveBytes", p.MaxObjectiveBytes, 16384}, {"maxResultBytes", p.MaxResultBytes, 65536}, {"maxTreeTokens", p.MaxTreeTokens, MaxTreeTokens}} {
		if v.n < 1 || v.n > v.max {
			return fmt.Errorf("%s must be between 1 and %d", v.name, v.max)
		}
	}
	return nil
}

type Profile struct {
	Tools           []string `json:"tools,omitempty"`
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Instructions    string   `json:"instructions"`
	Enabled         bool     `json:"enabled"`
	MaxOutputTokens int      `json:"maxOutputTokens"`
}

func DefaultProfile() Profile {
	return Profile{ID: "analysis", Name: "Analysis", Instructions: "Return a concise analysis of the supplied objective.", Enabled: true, MaxOutputTokens: 1024}
}
func (p Profile) Validate() error {
	if err := ValidateTools(p.Tools); err != nil {
		return err
	}
	if !ID.MatchString(p.ID) || strings.TrimSpace(p.Name) == "" || len(p.Name) > 120 {
		return errors.New("profile requires a lowercase id (max 63 bytes) and name (max 120 bytes)")
	}
	if !utf8.ValidString(p.Instructions) || strings.ContainsRune(p.Instructions, 0) || len(p.Instructions) > 4096 || strings.TrimSpace(p.Instructions) == "" {
		return errors.New("instructions must contain 1–4096 bytes of valid text")
	}
	if p.MaxOutputTokens < 1 || p.MaxOutputTokens > 4096 {
		return errors.New("profile maxOutputTokens must be between 1 and 4096")
	}
	return nil
}

type PolicyView struct {
	Policy    Policy `json:"policy"`
	Revision  int64  `json:"revision"`
	UpdatedBy string `json:"updatedBy,omitempty"`
}
type ProfileView struct {
	Profile   Profile `json:"profile"`
	Revision  int64   `json:"revision"`
	UpdatedBy string  `json:"updatedBy,omitempty"`
}

// Avoid duplicate revision fields when persisting an execution snapshot.
type Selection struct {
	Policy  PolicyView  `json:"policy"`
	Profile ProfileView `json:"profile"`
}
type Manager struct{ Store storage.Store }

func (m Manager) Policy(ctx context.Context) (PolicyView, error) {
	v := PolicyView{Policy: DefaultPolicy()}
	r, e := m.Store.GetSetting(ctx, PolicyKey)
	if errors.Is(e, storage.ErrNotFound) {
		return v, nil
	}
	if e != nil {
		return v, e
	}
	v.Policy = Policy{MaxTreeTokens: DefaultTreeTokens}
	if e = json.Unmarshal(r.Value, &v.Policy); e != nil {
		return v, errors.New("invalid stored delegation policy")
	}
	v.Revision = r.Revision
	v.UpdatedBy = r.UpdatedBy
	return v, v.Policy.Validate()
}
func (m Manager) Profile(ctx context.Context, id string) (ProfileView, error) {
	v := ProfileView{}
	if !ID.MatchString(id) {
		return v, storage.ErrNotFound
	}
	r, e := m.Store.GetSetting(ctx, ProfilePrefix+id)
	if errors.Is(e, storage.ErrNotFound) && id == "analysis" {
		return ProfileView{Profile: DefaultProfile()}, nil
	}
	if e != nil {
		return v, e
	}
	if e = json.Unmarshal(r.Value, &v.Profile); e != nil || v.Profile.ID != id {
		return v, errors.New("invalid stored delegation profile")
	}
	v.Revision = r.Revision
	v.UpdatedBy = r.UpdatedBy
	return v, v.Profile.Validate()
}
func (m Manager) Profiles(ctx context.Context) ([]ProfileView, error) {
	rows, e := m.Store.ListSettings(ctx, ProfilePrefix)
	if e != nil {
		return nil, e
	}
	a, e := m.Profile(ctx, "analysis")
	if e != nil {
		return nil, e
	}
	out := []ProfileView{a}
	for _, r := range rows {
		id := strings.TrimPrefix(r.Key, ProfilePrefix)
		if id == "analysis" {
			continue
		}
		p, e := m.Profile(ctx, id)
		if e != nil {
			return nil, e
		}
		out = append(out, p)
	}
	return out, nil
}
func (m Manager) SavePolicy(ctx context.Context, p Policy, rev int64, actor string) (PolicyView, error) {
	if e := p.Validate(); e != nil {
		return PolicyView{}, e
	}
	if rev < 0 {
		return PolicyView{}, storage.ErrConflict
	}
	b, e := json.Marshal(p)
	if e != nil {
		return PolicyView{}, e
	}
	next, e := m.Store.PutSetting(ctx, PolicyKey, b, rev, actor)
	return PolicyView{p, next, actor}, e
}
func (m Manager) SaveProfile(ctx context.Context, p Profile, rev int64, actor string) (ProfileView, error) {
	if e := p.Validate(); e != nil {
		return ProfileView{}, e
	}
	if rev < 0 {
		return ProfileView{}, storage.ErrConflict
	}
	b, e := json.Marshal(p)
	if e != nil {
		return ProfileView{}, e
	}
	next, e := m.Store.PutSetting(ctx, ProfilePrefix+p.ID, b, rev, actor)
	return ProfileView{p, next, actor}, e
}
func (m Manager) Select(ctx context.Context, id string) (Selection, error) {
	p, e := m.Policy(ctx)
	if e != nil {
		return Selection{}, e
	}
	if !p.Policy.Enabled {
		return Selection{}, errors.New("delegation is disabled by installation policy")
	}
	v, e := m.Profile(ctx, id)
	if e != nil {
		return Selection{}, e
	}
	if !v.Profile.Enabled {
		return Selection{}, errors.New("delegation profile is disabled")
	}
	return Selection{p, v}, nil
}

// Reserve serializes capacity admission across service instances. Counting all
// occupied slots also honors limits lowered while higher-numbered slots are busy.
func (m Manager) Reserve(ctx context.Context, holder string, limit int, ttl time.Duration) (string, error) {
	lock := "delegation/admission"
	lockHolder := domain.NewID("admission")
	acquired := false
	for i := 0; i < 100; i++ {
		ok, e := m.Store.AcquireLease(ctx, lock, lockHolder, 10*time.Second)
		if e != nil {
			return "", e
		}
		if ok {
			acquired = true
			break
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
	if !acquired {
		return "", errors.New("delegation admission is busy; retry later")
	}
	// Release failure is returned; the short lease is a crash-recovery fallback.
	slot, err := func() (string, error) {
		c, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		current, e := m.Policy(c)
		if e != nil {
			return "", e
		}
		if !current.Policy.Enabled {
			return "", errors.New("delegation is disabled by installation policy")
		}
		limit = min(limit, current.Policy.MaxConcurrentChildren)
		rows, e := m.Store.ListLeases(c, "delegation/slot/")
		if e != nil {
			return "", e
		}
		active := 0
		for _, l := range rows {
			if l.ExpiresAt.After(time.Now()) {
				active++
			}
		}
		if active >= limit {
			return "", errors.New("delegation: installation child capacity exhausted; retry later")
		}
		for i := 0; i < MaxSlots; i++ {
			key := fmt.Sprintf("delegation/slot/%d", i)
			ok, e := m.Store.AcquireLease(c, key, holder, ttl)
			if e != nil {
				return "", e
			}
			if ok {
				return key, nil
			}
		}
		return "", errors.New("delegation: installation child capacity exhausted; retry later")
	}()
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if e := m.Store.ReleaseLease(cleanup, lock, lockHolder); e != nil {
		if slot != "" {
			e = errors.Join(e, m.Store.ReleaseLease(cleanup, slot, holder))
		}
		return "", errors.Join(err, e)
	}
	return slot, err
}
