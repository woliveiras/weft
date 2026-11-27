package factory

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/woliveiras/weft/agents/eino/protocol"
)

var researchURL = regexp.MustCompile(`https?://\S+`)

func Researcher(ctx context.Context) (protocol.Agent, error) {
	return messageAgent(ctx, "research", map[string]action{"research.question": researchQuestion})
}

func researchQuestion(ctx context.Context, j *job) error {
	question := strings.TrimSpace(j.text("question"))
	if question == "" {
		return nil
	}
	qid := first(j.text("question_id"), j.message.ID)
	urls := researchURL.FindAllString(question, 3)
	if tmpl := stringValue(j.in.Config["search_url"]); len(urls) == 0 && tmpl != "" {
		urls = []string{strings.ReplaceAll(tmpl, "{q}", url.QueryEscape(question))}
	}
	var sources, notes strings.Builder
	for _, u := range urls {
		out, err := j.client.Tool(ctx, "http.fetch", map[string]string{"url": u})
		var status *protocol.HTTPError
		if errors.As(err, &status) && status.Status == http.StatusForbidden {
			if err := j.client.Output(ctx, "http.fetch not granted; answering from the model only"); err != nil {
				return err
			}
			break
		}
		if errors.Is(err, protocol.ErrToolFailed) {
			continue
		}
		if err != nil {
			return err
		}
		text := []rune(out.Content)
		fmt.Fprintf(&notes, "\nSource %s:\n%s\n", u, string(text[:min(len(text), 4000)]))
		fmt.Fprintf(&sources, "- %s\n", u)
	}
	answer, err := j.model(ctx, "Answer this research question concisely. Cite supplied sources.\nQuestion: "+question+"\n"+notes.String(), false)
	if err != nil {
		return err
	}
	if strings.TrimSpace(answer) == "" || strings.HasPrefix(answer, "MOCK:") {
		answer = "No model answer available (offline/mock provider). Question recorded for a human or a later run."
	}
	if sources.Len() == 0 {
		sources.WriteString("- (none)\n")
	}
	path := "research/" + qid + ".md"
	if err := j.write(ctx, path, "# "+question+"\n\n"+answer+"\n\n## Sources\n\n"+sources.String()); err != nil {
		return err
	}
	runes := []rune(answer)
	j.emit("research.done", map[string]string{"request_id": j.text("request_id"), "question_id": qid, "findings_path": path, "summary": string(runes[:min(300, len(runes))])})
	j.summary = qid + " answered"
	return nil
}
