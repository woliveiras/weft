package activity

import (
	"context"
	"time"

	"github.com/woliveiras/weft/server/internal/trigger"
)

type TriggerHold struct {
	Eligibility string    `json:"eligibility,omitempty"`
	TriggerID   string    `json:"triggerId"`
	TriggerName string    `json:"triggerName"`
	Workflow    string    `json:"workflow"`
	Key         string    `json:"key"`
	URL         string    `json:"url,omitempty"`
	Reason      string    `json:"reason"`
	Round       int       `json:"round"`
	State       string    `json:"state"`
	Publication string    `json:"publication,omitempty"`
	Disabled    bool      `json:"disabled,omitempty"`
	OpenedAt    time.Time `json:"openedAt"`
}

func (x *build) triggerHolds(ctx context.Context) (bool, *SectionError) {
	triggers, err := (trigger.Manager{Store: x.b.Store}).List(ctx, x.q.Workflow)
	if err != nil {
		return false, failed("triggerHolds", "Could not load issue readiness holds.", err)
	}
	for _, t := range triggers {
		if x.q.Agent != "" && t.NodeID != x.q.Agent {
			continue
		}
		for _, h := range t.Holds {
			if len(x.view.TriggerHolds) >= MaxNeedsYou {
				x.view.TriggerHoldsTruncated = true
				return true, nil
			}
			x.view.TriggerHolds = append(x.view.TriggerHolds, TriggerHold{TriggerID: t.ID, TriggerName: t.Name, Workflow: t.Workflow,
				Key: h.Key, URL: h.URL, Reason: trigger.ReadinessReason(h.Reason), Round: h.Round, State: h.State,
				Publication: h.Publication, Eligibility: h.Eligibility, Disabled: t.ReadinessDisabled, OpenedAt: h.OpenedAt})
		}
	}
	return false, nil
}
