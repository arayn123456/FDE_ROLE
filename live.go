package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

type boardKind string

const (
	boardGreenhouse boardKind = "greenhouse"
	boardLever      boardKind = "lever"
	boardAshby      boardKind = "ashby"
)

type careerBoard struct {
	Kind boardKind
	ID   string
}

var companyBoards = map[string][]careerBoard{
	"Palantir":           {{boardGreenhouse, "palantir"}, {boardGreenhouse, "palantirtech"}},
	"OpenAI":             {{boardGreenhouse, "openai"}, {boardAshby, "openai"}},
	"Anthropic":          {{boardGreenhouse, "anthropic"}},
	"Scale AI":           {{boardGreenhouse, "scaleai"}},
	"Anduril":            {{boardGreenhouse, "andurilindustries"}, {boardGreenhouse, "anduril"}},
	"Databricks":         {{boardGreenhouse, "databricks"}},
	"Stripe":             {{boardGreenhouse, "stripe"}},
	"Snowflake":          {{boardGreenhouse, "snowflake"}},
	"CrowdStrike":        {{boardGreenhouse, "crowdstrike"}},
	"Applied Intuition":  {{boardGreenhouse, "appliedintuition"}},
	"Shield AI":          {{boardGreenhouse, "shieldai"}},
	"C3.ai":              {{boardGreenhouse, "c3ai"}, {boardGreenhouse, "c3"}},
	"Helsing":            {{boardGreenhouse, "helsing"}, {boardAshby, "helsing"}},
	"Glean":              {{boardGreenhouse, "gleanwork"}, {boardAshby, "glean"}},
	"Tesla":              {{boardGreenhouse, "tesla"}, {boardLever, "tesla"}},
	"SpaceX":             {{boardGreenhouse, "spacex"}},
}

var fdeTitle = regexp.MustCompile(`(?i)forward[-\s]?deployed|field[-\s]?deployment|field[-\s]?deployed|\bfde\b`)

type greenhouseFeed struct {
	Jobs []struct {
		Title        string `json:"title"`
		AbsoluteURL  string `json:"absolute_url"`
		UpdatedAt    string `json:"updated_at"`
		Location     struct {
			Name string `json:"name"`
		} `json:"location"`
		Content string `json:"content"`
	} `json:"jobs"`
}

type leverJob struct {
	Text             string `json:"text"`
	HostedURL        string `json:"hostedUrl"`
	DescriptionPlain string `json:"descriptionPlain"`
	Categories       struct {
		Location string `json:"location"`
	} `json:"categories"`
}

type ashbyFeed struct {
	Jobs []struct {
		Title       string `json:"title"`
		JobURL      string `json:"jobUrl"`
		Description string `json:"descriptionPlain"`
		Location    string `json:"location"`
	} `json:"jobs"`
}

func fetchLiveCompany(client *http.Client, company string, fetchedAt time.Time) ([]Role, error) {
	boards := companyBoards[company]
	var out []Role
	var lastErr error
	seenBoard := false

	for _, b := range boards {
		roles, err := fetchBoard(client, company, b, fetchedAt)
		if err != nil {
			lastErr = err
			continue
		}
		seenBoard = true
		out = append(out, roles...)
		if len(roles) > 0 {
			break
		}
	}

	if !seenBoard && lastErr != nil && len(boards) > 0 {
		return nil, lastErr
	}
	return filterFDE(out), nil
}

func fetchBoard(client *http.Client, company string, b careerBoard, fetchedAt time.Time) ([]Role, error) {
	switch b.Kind {
	case boardGreenhouse:
		return fetchGreenhouse(client, company, b.ID, fetchedAt)
	case boardLever:
		return fetchLever(client, company, b.ID, fetchedAt)
	case boardAshby:
		return fetchAshby(client, company, b.ID, fetchedAt)
	default:
		return nil, fmt.Errorf("unknown board kind %s", b.Kind)
	}
}

func fetchGreenhouse(client *http.Client, company, token string, fetchedAt time.Time) ([]Role, error) {
	u := "https://boards-api.greenhouse.io/v1/boards/" + token + "/jobs?content=true"
	body, err := getJSON(client, u)
	if err != nil {
		return nil, err
	}
	var feed greenhouseFeed
	if err := json.Unmarshal(body, &feed); err != nil {
		return nil, err
	}
	out := make([]Role, 0, len(feed.Jobs))
	for _, job := range feed.Jobs {
		out = append(out, Role{
			Company:     company,
			Title:       strings.TrimSpace(job.Title),
			URL:         strings.TrimSpace(job.AbsoluteURL),
			Snippet:     collapseSpace(stripTags(job.Location.Name + " " + job.Content)),
			SourceHost:  "boards-api.greenhouse.io/" + token,
			SearchQuery: "greenhouse:" + token,
			FetchedAt:   fetchedAt,
		})
	}
	return out, nil
}

func fetchLever(client *http.Client, company, token string, fetchedAt time.Time) ([]Role, error) {
	u := "https://api.lever.co/v0/postings/" + token + "?mode=json"
	body, err := getJSON(client, u)
	if err != nil {
		return nil, err
	}
	var jobs []leverJob
	if err := json.Unmarshal(body, &jobs); err != nil {
		return nil, err
	}
	out := make([]Role, 0, len(jobs))
	for _, job := range jobs {
		out = append(out, Role{
			Company:     company,
			Title:       strings.TrimSpace(job.Text),
			URL:         strings.TrimSpace(job.HostedURL),
			Snippet:     collapseSpace(job.Categories.Location + " " + job.DescriptionPlain),
			SourceHost:  "api.lever.co/" + token,
			SearchQuery: "lever:" + token,
			FetchedAt:   fetchedAt,
		})
	}
	return out, nil
}

func fetchAshby(client *http.Client, company, token string, fetchedAt time.Time) ([]Role, error) {
	u := "https://api.ashbyhq.com/posting-api/job-board/" + token
	body, err := getJSON(client, u)
	if err != nil {
		return nil, err
	}
	var feed ashbyFeed
	if err := json.Unmarshal(body, &feed); err != nil {
		return nil, err
	}
	out := make([]Role, 0, len(feed.Jobs))
	for _, job := range feed.Jobs {
		out = append(out, Role{
			Company:     company,
			Title:       strings.TrimSpace(job.Title),
			URL:         strings.TrimSpace(job.JobURL),
			Snippet:     collapseSpace(job.Location + " " + job.Description),
			SourceHost:  "api.ashbyhq.com/" + token,
			SearchQuery: "ashby:" + token,
			FetchedAt:   fetchedAt,
		})
	}
	return out, nil
}

func getJSON(client *http.Client, rawURL string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "fde-roles/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("http %d from %s", resp.StatusCode, rawURL)
	}
	return body, nil
}

func filterFDE(roles []Role) []Role {
	out := make([]Role, 0, len(roles))
	for _, r := range roles {
		blob := r.Title + " " + r.Snippet
		if fdeTitle.MatchString(blob) {
			if len(r.Snippet) > 400 {
				r.Snippet = r.Snippet[:400] + "…"
			}
			out = append(out, r)
		}
	}
	return out
}

var tagRe = regexp.MustCompile(`<[^>]+>`)

func stripTags(s string) string {
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = tagRe.ReplaceAllString(s, " ")
	return s
}
