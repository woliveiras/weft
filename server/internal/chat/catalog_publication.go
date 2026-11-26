package chat

import "time"

const PublishAgent = "agent.publish"

type CatalogPublicationDraft struct {
	Agent        string
	Instructions string
}

func (d CatalogPublicationDraft) valid() bool {
	return shortLabel(d.Agent) && d.Instructions != "" && safeText(d.Instructions, 3000)
}

type CatalogPublication struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	PreviousVersion  int    `json:"previousVersion"`
	Revision         int64  `json:"revision"`
	DefinitionDigest string `json:"definitionDigest"`
	Before           string `json:"before"`
	After            string `json:"after"`
}

func NewCatalogPublicationReview(owner, session string, publication CatalogPublication, now time.Time) (*OperationReview, error) {
	if !shortLabel(publication.ID) || !shortLabel(publication.Name) || publication.PreviousVersion < 1 ||
		publication.Revision < 1 || publication.DefinitionDigest == "" ||
		!safeText(publication.Before, 3000) || publication.After == "" || !safeText(publication.After, 3000) {
		return nil, ErrInvalidPlan
	}
	return newReview(owner, session, now, OperationReview{Publication: &publication})
}
