package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/xuri/excelize/v2"
)

const googleSearchURL = "https://www.googleapis.com/customsearch/v1"

var defaultCompanies = []string{
	"Palantir",
	"OpenAI",
	"Anthropic",
	"Scale AI",
	"Anduril",
	"Databricks",
	"Stripe",
	"Google",
	"Meta",
	"Microsoft",
	"Amazon",
	"Tesla",
	"SpaceX",
	"Snowflake",
	"CrowdStrike",
	"Applied Intuition",
	"Shield AI",
	"C3.ai",
	"Helsing",
	"Glean",
}

type searchResponse struct {
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	Items []searchItem `json:"items"`
}

type searchItem struct {
	Title       string `json:"title"`
	Link        string `json:"link"`
	Snippet     string `json:"snippet"`
	DisplayLink string `json:"displayLink"`
}

type Role struct {
	Company     string
	Title       string
	URL         string
	Snippet     string
	SourceHost  string
	SearchQuery string
	FetchedAt   time.Time
}

func main() {
	outDir := flag.String("out", "output", "directory for per-company txt and Excel files")
	apiKey := flag.String("key", os.Getenv("GOOGLE_API_KEY"), "Google Custom Search API key (or GOOGLE_API_KEY)")
	cx := flag.String("cx", firstEnv("GOOGLE_CSE_ID", "GOOGLE_CX"), "Programmable Search Engine ID (or GOOGLE_CSE_ID)")
	companiesFlag := flag.String("companies", "", "comma-separated company names (default: built-in FDE-heavy list)")
	source := flag.String("source", "auto", "google, live, or auto (auto uses Google when keys exist, otherwise public job boards)")
	pages := flag.Int("pages", 1, "Google result pages per company (1-10, 10 results each)")
	timeout := flag.Duration("timeout", 20*time.Second, "HTTP timeout")
	flag.Parse()

	hasGoogle := strings.TrimSpace(*apiKey) != "" && strings.TrimSpace(*cx) != ""
	mode := strings.ToLower(strings.TrimSpace(*source))
	if mode == "auto" {
		if hasGoogle {
			mode = "google"
		} else {
			mode = "live"
		}
	}
	if mode == "google" && !hasGoogle {
		fmt.Fprintln(os.Stderr, missingKeysHelp())
		os.Exit(1)
	}
	if *pages < 1 {
		*pages = 1
	}
	if *pages > 10 {
		*pages = 10
	}

	companies := defaultCompanies
	if strings.TrimSpace(*companiesFlag) != "" {
		companies = splitCompanies(*companiesFlag)
	}

	client := &http.Client{Timeout: *timeout}
	fetchedAt := time.Now().UTC()
	byCompany := make(map[string][]Role)
	var all []Role

	switch mode {
	case "google":
		fmt.Printf("Fetching live Google results for %d companies...\n", len(companies))
	case "live":
		fmt.Printf("Google API keys not set — fetching live FDE jobs from public career boards for %d companies...\n", len(companies))
	default:
		fatalf("unknown -source %q (use google, live, or auto)", *source)
	}

	for i, company := range companies {
		fmt.Printf("[%d/%d] %s\n", i+1, len(companies), company)
		var (
			roles []Role
			err   error
		)
		if mode == "google" {
			query := fdeQuery(company)
			roles, err = searchCompany(client, *apiKey, *cx, company, query, *pages, fetchedAt)
		} else {
			roles, err = fetchLiveCompany(client, company, fetchedAt)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "  warning: %s: %v\n", company, err)
			continue
		}
		roles = dedupe(roles)
		byCompany[company] = roles
		all = append(all, roles...)
		fmt.Printf("  found %d listings\n", len(roles))
		if i < len(companies)-1 {
			time.Sleep(250 * time.Millisecond)
		}
	}

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fatalf("create output dir: %v", err)
	}

	for _, company := range companies {
		roles := byCompany[company]
		slug := slugify(company)
		txtPath := filepath.Join(*outDir, slug+".txt")
		xlsxPath := filepath.Join(*outDir, slug+".xlsx")
		if err := writeTXT(txtPath, company, roles); err != nil {
			fatalf("write %s: %v", txtPath, err)
		}
		if err := writeXLSX(xlsxPath, company, roles); err != nil {
			fatalf("write %s: %v", xlsxPath, err)
		}
	}

	all = dedupe(all)
	sort.Slice(all, func(i, j int) bool {
		if all[i].Company == all[j].Company {
			return all[i].Title < all[j].Title
		}
		return all[i].Company < all[j].Company
	})

	allTxt := filepath.Join(*outDir, "all_fde_roles.txt")
	allXlsx := filepath.Join(*outDir, "all_fde_roles.xlsx")
	if err := writeTXT(allTxt, "All companies", all); err != nil {
		fatalf("write combined txt: %v", err)
	}
	if err := writeXLSX(allXlsx, "All FDE roles", all); err != nil {
		fatalf("write combined excel: %v", err)
	}

	fmt.Printf("\nSaved %d unique listings.\n", len(all))
	fmt.Printf("Per-company files: %s\\<company>.txt and %s\\<company>.xlsx\n", *outDir, *outDir)
	fmt.Printf("Combined files:    %s\n                   %s\n", allTxt, allXlsx)
}

func searchCompany(client *http.Client, apiKey, cx, company, query string, pages int, fetchedAt time.Time) ([]Role, error) {
	var out []Role
	for page := 0; page < pages; page++ {
		start := page*10 + 1
		items, err := googleSearch(client, apiKey, cx, query, start)
		if err != nil {
			return out, err
		}
		if len(items) == 0 {
			break
		}
		for _, item := range items {
			if strings.TrimSpace(item.Link) == "" {
				continue
			}
			out = append(out, Role{
				Company:     company,
				Title:       strings.TrimSpace(item.Title),
				URL:         strings.TrimSpace(item.Link),
				Snippet:     collapseSpace(item.Snippet),
				SourceHost:  strings.TrimSpace(item.DisplayLink),
				SearchQuery: query,
				FetchedAt:   fetchedAt,
			})
		}
		if len(items) < 10 {
			break
		}
	}
	return out, nil
}

func googleSearch(client *http.Client, apiKey, cx, query string, start int) ([]searchItem, error) {
	params := url.Values{}
	params.Set("key", apiKey)
	params.Set("cx", cx)
	params.Set("q", query)
	params.Set("num", "10")
	params.Set("start", fmt.Sprintf("%d", start))
	params.Set("hl", "en")

	req, err := http.NewRequest(http.MethodGet, googleSearchURL+"?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}

	var parsed searchResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("decode google response: %w", err)
	}
	if parsed.Error != nil {
		return nil, fmt.Errorf("google api %d: %s", parsed.Error.Code, parsed.Error.Message)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return parsed.Items, nil
}

func fdeQuery(company string) string {
	return fmt.Sprintf(
		`"%s" ("forward deployed engineer" OR "forward-deployed engineer" OR "field deployment engineer" OR "FDE") (job OR jobs OR careers OR hiring)`,
		company,
	)
}

func writeTXT(path, heading string, roles []Role) error {
	var b strings.Builder
	b.WriteString("FDE roles — " + heading + "\n")
	b.WriteString("Fetched (UTC): " + time.Now().UTC().Format(time.RFC3339) + "\n")
	b.WriteString("Count: " + fmt.Sprintf("%d", len(roles)) + "\n")
	b.WriteString(strings.Repeat("-", 72) + "\n\n")

	if len(roles) == 0 {
		b.WriteString("No Google listings found for this company.\n")
	}
	for i, r := range roles {
		fmt.Fprintf(&b, "%d. %s\n", i+1, r.Title)
		fmt.Fprintf(&b, "   Company:  %s\n", r.Company)
		fmt.Fprintf(&b, "   Host:     %s\n", r.SourceHost)
		fmt.Fprintf(&b, "   URL:      %s\n", r.URL)
		if r.Snippet != "" {
			fmt.Fprintf(&b, "   Snippet:  %s\n", r.Snippet)
		}
		fmt.Fprintf(&b, "   Query:    %s\n\n", r.SearchQuery)
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func writeXLSX(path, sheetName string, roles []Role) error {
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	sheet := sanitizeSheetName(sheetName)
	if err := f.SetSheetName("Sheet1", sheet); err != nil {
		sheet = "Sheet1"
	}

	headers := []string{"Company", "Title", "URL", "Host", "Snippet", "Search query", "Fetched at (UTC)"}
	headerStyle, err := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "FFFFFF"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"1F4E79"}, Pattern: 1},
		Alignment: &excelize.Alignment{WrapText: true, Vertical: "center"},
	})
	if err != nil {
		return err
	}
	wrapStyle, err := f.NewStyle(&excelize.Style{
		Alignment: &excelize.Alignment{WrapText: true, Vertical: "top"},
	})
	if err != nil {
		return err
	}

	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		if err := f.SetCellValue(sheet, cell, h); err != nil {
			return err
		}
	}
	if err := f.SetRowStyle(sheet, 1, 1, headerStyle); err != nil {
		return err
	}
	if err := f.SetRowHeight(sheet, 1, 22); err != nil {
		return err
	}

	for i, r := range roles {
		row := i + 2
		values := []any{
			r.Company,
			r.Title,
			r.URL,
			r.SourceHost,
			r.Snippet,
			r.SearchQuery,
			r.FetchedAt.Format(time.RFC3339),
		}
		for col, v := range values {
			cell, _ := excelize.CoordinatesToCellName(col+1, row)
			if err := f.SetCellValue(sheet, cell, v); err != nil {
				return err
			}
		}
		if err := f.SetRowStyle(sheet, row, row, wrapStyle); err != nil {
			return err
		}
	}

	_ = f.SetColWidth(sheet, "A", "A", 22)
	_ = f.SetColWidth(sheet, "B", "B", 48)
	_ = f.SetColWidth(sheet, "C", "C", 46)
	_ = f.SetColWidth(sheet, "D", "D", 24)
	_ = f.SetColWidth(sheet, "E", "E", 60)
	_ = f.SetColWidth(sheet, "F", "F", 40)
	_ = f.SetColWidth(sheet, "G", "G", 22)

	_ = f.AutoFilter(sheet, "A1:G1", nil)

	return f.SaveAs(path)
}

func dedupe(roles []Role) []Role {
	seen := make(map[string]struct{}, len(roles))
	out := make([]Role, 0, len(roles))
	for _, r := range roles {
		key := strings.ToLower(strings.TrimSpace(r.URL))
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, r)
	}
	return out
}

func splitCompanies(raw string) []string {
	parts := strings.Split(raw, ",")
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func firstEnv(keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "company"
	}
	return out
}

func sanitizeSheetName(name string) string {
	invalid := regexp.MustCompile(`[\\/?*\[\]]`)
	name = invalid.ReplaceAllString(name, " ")
	name = strings.TrimSpace(name)
	if name == "" {
		return "Roles"
	}
	if len(name) > 31 {
		name = name[:31]
	}
	return name
}

func collapseSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func missingKeysHelp() string {
	return strings.TrimSpace(`
Google Custom Search credentials are missing.

This program uses Google's official Custom Search JSON API (live web results).
HTML scraping of google.com is blocked and unstable, so an API key is required.

Setup:
  1. Create a Google Cloud project and enable "Custom Search API"
     https://console.cloud.google.com/apis/library/customsearch.googleapis.com
  2. Create an API key
     https://console.cloud.google.com/apis/credentials
  3. Create a Programmable Search Engine that searches the entire web
     https://programmablesearchengine.google.com/
     Copy the Search engine ID (cx)

Then run:
  $env:GOOGLE_API_KEY="your-key"
  $env:GOOGLE_CSE_ID="your-cx"
  go run .

Optional:
  go run . -companies "Palantir,OpenAI,Anthropic" -pages 2 -out output
`)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
