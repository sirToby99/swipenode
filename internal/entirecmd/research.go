package entirecmd

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/sirToby99/swipenode/internal/entirectx"
	"github.com/sirToby99/swipenode/internal/gitcontext"
	"github.com/spf13/cobra"
)

const researchSchemaVersion = "swipenode.research.v1"

var researchURLPattern = regexp.MustCompile(`https?://[^\s<>"'\x60)\]}]+`)

type researchRepository struct {
	Root   string `json:"root,omitempty"`
	Branch string `json:"branch,omitempty"`
	Head   string `json:"head,omitempty"`
}

type researchSource struct {
	URL         string   `json:"url"`
	Title       string   `json:"title,omitempty"`
	SourceType  string   `json:"source_type"`
	RetrievedAt string   `json:"retrieved_at,omitempty"`
	Excerpts    []string `json:"excerpts"`
	Warnings    []string `json:"warnings"`
	Error       string   `json:"error,omitempty"`
}

type researchReport struct {
	SchemaVersion string             `json:"schema_version"`
	Question      string             `json:"question"`
	Mode          string             `json:"mode"`
	Repository    researchRepository `json:"repository"`
	Sources       []researchSource   `json:"sources"`
	Limitations   []string           `json:"limitations"`
}

func newResearchCommand(_ BuildInfo, deps Dependencies) *cobra.Command {
	var asJSON bool
	var explicitSources []string
	command := &cobra.Command{
		Use:   "research <question>",
		Short: "Retrieve source-backed excerpts from explicit public sources",
		Long:  "Research uses SwipeNode's existing local extraction engine. Provide one or more --source URLs, or include public URLs in the question. It does not add a search provider or LLM.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sources, err := normalizeSources(explicitSources, args[0])
			if err != nil {
				return err
			}
			report, successCount := runResearch(cmd.Context(), args[0], sources, deps)
			if asJSON {
				err = writeIndentedJSON(cmd, report)
			} else {
				err = writeResearchHuman(cmd, report)
			}
			if err != nil {
				return err
			}
			if successCount == 0 {
				return fmt.Errorf("no research source could be retrieved")
			}
			return nil
		},
	}
	command.Flags().StringArrayVar(&explicitSources, "source", nil, "explicit public source URL (repeatable)")
	command.Flags().BoolVar(&asJSON, "json", false, "emit stable machine-readable JSON")
	return command
}

func normalizeSources(explicit []string, question string) ([]string, error) {
	values := append([]string{}, explicit...)
	values = append(values, researchURLPattern.FindAllString(question, -1)...)
	seen := map[string]struct{}{}
	var result []string
	for _, value := range values {
		value = strings.TrimRight(strings.TrimSpace(value), ".,;:")
		parsed, err := url.ParseRequestURI(value)
		if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.User != nil {
			return nil, fmt.Errorf("invalid public research source %q", value)
		}
		if _, ok := seen[value]; !ok {
			seen[value] = struct{}{}
			result = append(result, value)
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("local research requires an explicit public URL via --source or in the question; no search provider or LLM is configured")
	}
	return result, nil
}

func runResearch(ctx context.Context, question string, sources []string, deps Dependencies) (researchReport, int) {
	report := researchReport{
		SchemaVersion: researchSchemaVersion, Question: question,
		Mode: "local_explicit_sources", Sources: []researchSource{},
		Limitations: []string{
			"SwipeNode retrieves explicit public HTML sources without JavaScript and does not synthesize an LLM answer.",
			"Repository metadata may be attached, but repository file contents are never uploaded.",
		},
	}
	if cwd, err := deps.Getwd(); err == nil {
		entire := entirectx.FromLookup(deps.Getenv)
		if root, rootErr := entirectx.ResolveRepoRoot(entire, cwd); rootErr == nil {
			if repository, inspectErr := gitcontext.Inspect(ctx, root); inspectErr == nil {
				report.Repository = researchRepository{Root: root, Branch: repository.Branch, Head: repository.Head}
			}
		}
	}
	terms := researchTerms(question)
	successCount := 0
	for _, sourceURL := range sources {
		source := researchSource{URL: sourceURL, SourceType: "public_html", Excerpts: []string{}, Warnings: []string{}}
		result, err := deps.Extract(ctx, sourceURL)
		if err != nil {
			source.Error = err.Error()
			report.Sources = append(report.Sources, source)
			continue
		}
		successCount++
		source.URL = result.URL
		source.Title = result.Title
		source.RetrievedAt = result.FetchedAt
		source.Warnings = append(source.Warnings, result.Warnings...)
		source.Excerpts = relevantExcerpts(result.Content, terms)
		report.Sources = append(report.Sources, source)
	}
	return report, successCount
}

func researchTerms(question string) []string {
	stop := map[string]struct{}{
		"about": {}, "does": {}, "from": {}, "have": {}, "into": {}, "that": {}, "the": {}, "their": {},
		"this": {}, "what": {}, "when": {}, "where": {}, "which": {}, "with": {}, "sagt": {}, "und": {}, "wie": {},
		"http": {}, "https": {}, "www": {}, "com": {}, "org": {}, "html": {},
	}
	parts := strings.FieldsFunc(strings.ToLower(question), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	set := map[string]struct{}{}
	for _, part := range parts {
		if len(part) < 3 {
			continue
		}
		if _, ignored := stop[part]; !ignored {
			set[part] = struct{}{}
		}
	}
	terms := make([]string, 0, len(set))
	for term := range set {
		terms = append(terms, term)
	}
	sort.Strings(terms)
	return terms
}

func relevantExcerpts(content string, terms []string) []string {
	type scored struct {
		text  string
		score int
		index int
	}
	paragraphs := strings.Split(content, "\n")
	var candidates []scored
	for index, paragraph := range paragraphs {
		paragraph = strings.Join(strings.Fields(paragraph), " ")
		if paragraph == "" {
			continue
		}
		lower := strings.ToLower(paragraph)
		score := 0
		for _, term := range terms {
			if strings.Contains(lower, term) {
				score++
			}
		}
		candidates = append(candidates, scored{text: excerptText(paragraph, 320), score: score, index: index})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score == candidates[j].score {
			return candidates[i].index < candidates[j].index
		}
		return candidates[i].score > candidates[j].score
	})
	limit := 3
	if len(candidates) < limit {
		limit = len(candidates)
	}
	result := make([]string, 0, limit)
	for _, candidate := range candidates[:limit] {
		result = append(result, candidate.text)
	}
	return result
}

func excerptText(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return strings.TrimSpace(value[:limit-1]) + "…"
}

func writeResearchHuman(cmd *cobra.Command, report researchReport) error {
	out := cmd.OutOrStdout()
	if _, err := fmt.Fprintf(out, "Question: %s\nMode: %s\n", report.Question, report.Mode); err != nil {
		return err
	}
	if report.Repository.Root != "" {
		if _, err := fmt.Fprintf(out, "Repository context: %s (%s, %s)\n", report.Repository.Root, report.Repository.Branch, report.Repository.Head); err != nil {
			return err
		}
	}
	for index, source := range report.Sources {
		if _, err := fmt.Fprintf(out, "\nSource %d: %s\n", index+1, source.URL); err != nil {
			return err
		}
		if source.Title != "" {
			_, _ = fmt.Fprintln(out, "Title:", source.Title)
		}
		if source.Error != "" {
			_, _ = fmt.Fprintln(out, "Error:", source.Error)
			continue
		}
		for _, value := range source.Excerpts {
			_, _ = fmt.Fprintln(out, "-", value)
		}
	}
	return nil
}
