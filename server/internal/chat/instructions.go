package chat

import (
	"errors"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
)

const EditInstructions = "workflow.instructions"

type InstructionEdit struct {
	Graph          string `json:"graph"`
	Node           string `json:"node"`
	After          string `json:"after"`
	Before         string `json:"before"`
	Version        int    `json:"version"`
	SourceDigest   string `json:"sourceDigest"`
	CatalogID      string `json:"catalogId,omitempty"`
	CatalogVersion int    `json:"catalogVersion,omitempty"`
}

func (p InstructionEdit) ValidInput() bool {
	return shortLabel(p.Graph) && !strings.HasPrefix(p.Graph, domain.TaskRunGraphPrefix) && shortLabel(p.Node) &&
		safeText(p.After, 3000)
}

func NewInstructionReview(owner, session string, p InstructionEdit, now time.Time) (*OperationReview, error) {
	if !p.ValidInput() || !safeText(p.Before, 3000) || p.Version < 1 || len(p.SourceDigest) != 64 {
		return nil, errors.New("invalid or sensitive workflow instruction change")
	}
	return newReview(owner, session, now, OperationReview{Instructions: &p})
}
