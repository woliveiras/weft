package prefs

import "fmt"

// DefaultTheme is the theme of users who never chose one.
const DefaultTheme = "light"

var themes = map[string]bool{"light": true, "dark": true, "contrast": true}

// Service manages the theme of each user.
type Service struct {
	store Store
	cache *Cache
}

// NewService returns a Service backed by store.
func NewService(store Store) *Service {
	return &Service{store: store, cache: NewCache(store)}
}

func themeKey(user string) string { return "theme/" + user }

// Theme returns the user's theme, or DefaultTheme.
func (s *Service) Theme(user string) string {
	if v, ok := s.cache.Get(themeKey(user)); ok {
		return v
	}
	return DefaultTheme
}

// SetTheme changes the user's theme.
func (s *Service) SetTheme(user, theme string) error {
	if !themes[theme] {
		return fmt.Errorf("prefs: unknown theme %q", theme)
	}
	s.store.Set(themeKey(user), theme)
	return nil
}

// ResetTheme returns the user to DefaultTheme.
func (s *Service) ResetTheme(user string) {
	s.store.Delete(themeKey(user))
}
