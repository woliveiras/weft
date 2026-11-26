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
var possiblePRURL = regexp.MustCompile(`(?i)\b(?:[a-z][a-z0-9+.-]*://[^\s<>\x22\x27\x60]*|(?:[a-z0-9-]+\.)+[a-z0-9-]+(?::[0-9]+)?/[^/\s<>\x22\x27\x60]+/[^\s<>\x22\x27\x60]+)`)
var possiblePRRepository = regexp.MustCompile(`[A-Za-z0-9_.-]+(?:/[A-Za-z0-9_.-]+)+`)

// The model interprets the request. Its scope hints are accepted only when
// the operator supplied them, or explicitly referred to a saved choice.
func groundChatPRPlan(plan chat.Plan, input, request string, session *chat.Session, actor Identity) (chat.Plan, string) {
	if plan.Kind != chat.AnalyzePR {
		return plan, ""
	}
	selector := &plan.PRSelector
	selector.ConnectionRevision, selector.WorkflowVersion, selector.Run = "", 0, ""
	supplied, repository, clarification := readExplicitPRScope(input)
	if clarification != "" {
		return plan, clarification
	}
	selector.URL, selector.Repository = supplied, repository
	for _, value := range []*string{&selector.Connection, &selector.Workflow, &selector.Node} {
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

// Explicit scope survives an incomplete model proposal. Remove URL spans
// before finding repository names so path segments cannot double as scopes.
func readExplicitPRScope(input string) (link, repository, clarification string) {
	for _, word := range possiblePRURL.FindAllString(input, -1) {
		candidate := strings.TrimRight(word, "()[],.!?;")
		if _, ok := chat.ReadPRURL(candidate); !ok {
			return "", "", "I could not recognize the supplied link as a supported pull request URL. Enter the full PR link or specify the repository as owner/name."
		}
		if link != "" && link != candidate {
			return "", "", "Which of the supplied pull requests should I review?"
		}
		link = candidate
	}
	withoutLinks := possiblePRURL.ReplaceAllString(input, " ")
	for _, span := range possiblePRRepository.FindAllStringIndex(withoutLinks, -1) {
		if !prScopeBounds(withoutLinks, span[0], span[1]) {
			continue
		}
		candidate := strings.TrimRight(withoutLinks[span[0]:span[1]], ".")
		if !chat.ValidPRRepository(candidate) || repository != "" && !strings.EqualFold(candidate, repository) {
			return "", "", "Which repository should I use? Enter one owner/name or the full PR link."
		}
		repository = candidate
	}
	return link, repository, ""
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
		if prScopeBounds(text, i, end) {
			return true
		}
		offset = end
	}
	return false
}

func prScopeBounds(text string, start, end int) bool {
	before, _ := utf8.DecodeLastRuneInString(text[:start])
	after, width := utf8.DecodeRuneInString(text[end:])
	if after == '.' {
		next, _ := utf8.DecodeRuneInString(text[end+width:])
		if !prScopeRune(next) {
			after = ' '
		}
	}
	return !prScopeRune(before) && !prScopeRune(after)
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
