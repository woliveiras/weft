package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/storage"
)

const chatModelSetting = "chat/model"

var chatModelName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:/@+-]{0,199}$`)

type chatModelSettings struct {
	Model    *chatModelSelection `json:"model,omitempty"`
	Revision int64               `json:"revision"`
	Status   string              `json:"status"`
}

type chatModelSelection struct {
	Provider string `json:"provider"`
	Name     string `json:"name"`
}

func validateChatModel(ref chatModelSelection) error {
	if ref.Provider == "" || !chatModelName.MatchString(ref.Provider) || !chatModelName.MatchString(ref.Name) ||
		strings.Contains(ref.Name, "://") || strings.HasPrefix(ref.Name, "sk-") || strings.HasPrefix(ref.Name, "ghp_") {
		return badRequest("select a configured provider and enter its model identifier")
	}
	return nil
}

func chatModelStatus(models *llm.Registry, ref chatModelSelection) string {
	if models == nil {
		return "unavailable"
	}
	// Get requires the explicit stored name. Resolve would let the "default"
	// alias silently pick another provider after the selected instance disappears.
	provider, ok := models.Get(ref.Provider)
	if !ok || llm.UnavailableReason(provider) != nil {
		return "unavailable"
	}
	return "configured"
}

func (s *Server) readChatModel(ctx context.Context) (chatModelSettings, error) {
	current, err := s.Store.GetSetting(ctx, chatModelSetting)
	if errors.Is(err, storage.ErrNotFound) {
		return chatModelSettings{Status: "unconfigured"}, nil
	}
	if err != nil {
		return chatModelSettings{}, err
	}
	var ref chatModelSelection
	if err := json.Unmarshal(current.Value, &ref); err != nil || validateChatModel(ref) != nil {
		return chatModelSettings{}, errors.New("stored Chat model selection is invalid")
	}
	return chatModelSettings{
		Model: &ref, Revision: current.Revision, Status: chatModelStatus(s.Models, ref),
	}, nil
}

func (s *Server) getChatSettings(w http.ResponseWriter, r *http.Request) error {
	settings, err := s.readChatModel(r.Context())
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, settings)
	return nil
}

func (s *Server) saveChatSettings(w http.ResponseWriter, r *http.Request) error {
	match := r.Header.Get("If-Match")
	var expected int64
	if match != "0" && match != `"0"` {
		var err error
		if expected, err = ifMatchRevision(r); err != nil {
			return err
		}
		if expected == 0 {
			return badRequest("If-Match is required, including zero for the initial model selection")
		}
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	dec.DisallowUnknownFields()
	var ref chatModelSelection
	if err := dec.Decode(&ref); err != nil || dec.Decode(new(any)) != io.EOF {
		return badRequest("expected one Chat model selection")
	}
	if err := validateChatModel(ref); err != nil {
		return err
	}
	if chatModelStatus(s.Models, ref) != "configured" {
		return badRequest("select an available model provider")
	}
	payload, err := json.Marshal(ref)
	if err != nil {
		return err
	}
	revision, err := s.Store.PutSetting(r.Context(), chatModelSetting, payload, expected, identityFrom(r.Context()).Name)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, chatModelSettings{
		Model: &ref, Revision: revision, Status: chatModelStatus(s.Models, ref),
	})
	return nil
}
