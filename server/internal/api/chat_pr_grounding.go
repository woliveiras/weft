package api

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/config"
)

var explicitPRNumber = regexp.MustCompile(`(?i)\b(?:pr|pull[ \t]+request)[ \t]*#?[ \t]*([0-9]+)\b`)
var possiblePRURL = regexp.MustCompile(`https://[^\s<>\x22\x27\x60]+`)

// The model interprets the request. Its scope hints are accepted only when
// the operator supplied them, or explicitly referred to a saved choice.
func groundChatPRPlan(plan chat.Plan, input, request string, session *chat.Session, actor Identity) (chat.Plan, string) {
	if plan.Kind != chat.AnalyzePR {
		return plan, ""
	}
	selector := &plan.PRSelector
	selector.ConnectionRevision, selector.WorkflowVersion, selector.Run = "", 0, ""
	var supplied string
	for _, word := range possiblePRURL.FindAllString(input, -1) {
		url := strings.TrimRight(word, "()[],.!?;")
		if _, ok := chat.ReadPRURL(url); !ok {
			continue
		}
		if supplied != "" && supplied != url {
			return plan, "Which of the supplied pull requests should I review?"
		}
		supplied = url
	}
	selector.URL = supplied
	for _, value := range []*string{&selector.Connection, &selector.Repository, &selector.Workflow, &selector.Node} {
		if *value != "" && !hasPRScopeMention(input, *value) {
			*value = ""
		}
	}
	if supplied != "" {
		locator, _ := chat.ReadPRURL(supplied)
		if selector.Number != "" && selector.Number != locator.Number || selector.Repository != "" && !strings.EqualFold(selector.Repository, locator.Repository) {
			return plan, "The PR number or repository differs from the supplied link. Which pull request should I review?"
		}
		selector.Number = locator.Number
	}
	for _, number := range explicitPRNumber.FindAllStringSubmatch(input, -1) {
		if number[1] != selector.Number {
			return plan, "The proposed PR number differs from the request. Which pull request should I review?"
		}
	}
	if selector.Connection == "" && selector.Repository == "" && selector.URL == "" && prRefersToSelection(input) {
		if !actor.Role.Allows(config.RoleOperator) {
			return plan, "An operator is required to discover or analyze pull requests."
		}
		selection, revision := priorPRConnection(session, request, actor.Role)
		if selection == "" {
			return plan, "Which repository should I use? Enter its owner/name, connection name or PR link."
		}
		selector.Connection, selector.ConnectionRevision = selection, revision
	}
	if !selector.Valid() {
		return plan, "Which pull request should I review? Enter its number or full PR link."
	}
	return plan, ""
}

func hasPRScopeMention(text, value string) bool {
	text, value = strings.ToLower(text), strings.ToLower(value)
	for offset := 0; offset < len(text); {
		i := strings.Index(text[offset:], value)
		if i < 0 {
			return false
		}
		i += offset
		end := i + len(value)
		before, _ := utf8.DecodeLastRuneInString(text[:i])
		after, width := utf8.DecodeRuneInString(text[end:])
		if after == '.' {
			next, _ := utf8.DecodeRuneInString(text[end+width:])
			if !prScopeRune(next) {
				after = ' '
			}
		}
		if !prScopeRune(before) && !prScopeRune(after) {
			return true
		}
		offset = end
	}
	return false
}

func prScopeRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("_-/.", r)
}

func prRefersToSelection(input string) bool {
	text := strings.ToLower(strings.Join(strings.Fields(input), " "))
	for _, phrase := range []string{"that repository", "same repository", "this repository", "that repo", "same repo", "this repo", "same connection", "that connection", "nesse repositório", "neste repositório", "mesmo repositório", "nesse repo", "neste repo", "mesmo repo", "nessa conexão", "nesta conexão", "mesma conexão"} {
		if hasPRScopeMention(text, phrase) {
			return true
		}
	}
	return false
}

func priorPRConnection(session *chat.Session, request string, role config.Role) (string, string) {
	var name, revision string
	for _, message := range session.Messages {
		if message.ID == request {
			break
		}
		pick := message.Selection
		if pick == nil || pick.Area != "scm" || !role.Allows(pick.Role) {
			continue
		}
		name, revision = pick.ID, ""
		if r := message.Review; r != nil && r.PR != nil && r.PR.Connection == name {
			revision = r.PR.ConnectionRevision
		}
		if d := message.Discovery; d != nil && d.Plan.PRSelector.Connection == name {
			revision = d.Plan.PRSelector.ConnectionRevision
		}
	}
	return name, revision
}
