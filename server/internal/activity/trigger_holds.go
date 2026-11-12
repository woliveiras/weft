package activity

import (
	"context"
	"slices"
	"time"

	"github.com/woliveiras/weft/server/internal/trigger"
)

type TriggerHold struct {
	ResolvedAt  *time.Time `json:"resolvedAt,omitempty"`
	Eligibility string     `json:"eligibility,omitempty"`
	TriggerID   string     `json:"triggerId"`
	TriggerName string     `json:"triggerName"`
	Workflow    string     `json:"workflow"`
	Key         string     `json:"key"`
	URL         string     `json:"url,omitempty"`
	Reason      string     `json:"reason"`
	Round       int        `json:"round"`
	State       string     `json:"state"`
	Publication string     `json:"publication,omitempty"`
	Disabled    bool       `json:"disabled,omitempty"`
	OpenedAt    time.Time  `json:"openedAt"`
}

func (x *build) triggerHolds(ctx context.Context) (bool, *SectionError) {
	triggers, err := (trigger.Manager{Store: x.b.Store}).List(ctx, x.q.Workflow)
	if err != nil {
		return false, failed("triggerHolds", "Could not load issue readiness holds.", err)
	}
	var resolved []TriggerHold
	for _, t := range triggers {
		if x.q.Agent != "" && t.NodeID != x.q.Agent {
			continue
		}
		for _, h := range t.HoldResolutions {
			if !h.ResolvedAt.Before(x.since) {
				resolved = append(resolved, TriggerHold{TriggerID: t.ID, TriggerName: t.Name, Workflow: t.Workflow,
					Key: h.Key, URL: h.URL, Reason: h.Reason, Round: h.Round, State: "resolved", ResolvedAt: &h.ResolvedAt})
			}
		}
		for _, h := range t.Holds {
			if len(x.view.TriggerHolds) >= MaxNeedsYou {
				x.view.TriggerHoldsTruncated = true
				break
			}
			x.view.TriggerHolds = append(x.view.TriggerHolds, TriggerHold{TriggerID: t.ID, TriggerName: t.Name, Workflow: t.Workflow,
				Key: h.Key, URL: h.URL, Reason: trigger.ReadinessReason(h.Reason), Round: h.Round, State: h.State,
				Publication: h.Publication, Eligibility: h.Eligibility, Disabled: t.ReadinessDisabled, OpenedAt: h.OpenedAt})
		}
	}
	slices.SortStableFunc(resolved, func(a, b TriggerHold) int { return b.ResolvedAt.Compare(*a.ResolvedAt) })
	x.view.ResolvedTriggerHolds = resolved[:min(len(resolved), MaxNeedsYou)]
	return x.view.TriggerHoldsTruncated || len(resolved) > MaxNeedsYou, nil
}
