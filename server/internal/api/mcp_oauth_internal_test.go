package api

import (
	"github.com/woliveiras/weft/server/internal/config"
	"testing"
)

func TestOAuthCallbackOrigin(t *testing.T) {
	for _, base := range []string{"http://localhost:8080", "http://127.0.0.1:8080", "https://weft.example"} {
		s := &Server{Deps: Deps{Config: &config.Config{Server: config.Server{PublicURL: base}}}}
		got, err := s.oauthBase()
		if err != nil || got != base {
			t.Fatal(got, err)
		}
	}
	for _, base := range []string{"", "http://weft.example", "https://user:secret@weft.example", "https://weft.example?next=evil", "https://weft.example/#x", "https://weft.example/path"} {
		s := &Server{Deps: Deps{Config: &config.Config{Server: config.Server{PublicURL: base}}}}
		if _, err := s.oauthBase(); err == nil {
			t.Fatal("invalid callback origin accepted")
		}
	}
}
