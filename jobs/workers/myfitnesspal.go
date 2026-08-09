package workers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/riverqueue/river"
)

type MyFitnessPalArgs struct{}

func (MyFitnessPalArgs) Kind() string { return "myfitnesspal" }

type MyFitnessPalWorker struct {
	river.WorkerDefaults[MyFitnessPalArgs]
	MFPCookie  string
	APIBaseURL string
}

func (w *MyFitnessPalWorker) Work(ctx context.Context, job *river.Job[MyFitnessPalArgs]) error {
	if w.MFPCookie == "" {
		slog.Warn("MFP_COOKIE not set — skipping MyFitnessPal job")
		return nil
	}

	entries, err := fetchMFPNutrition(ctx, w.MFPCookie)
	if err != nil {
		return fmt.Errorf("fetch MFP: %w", err)
	}
	slog.Info("MFP fetch complete", "entries", len(entries))
	return postNutrition(ctx, w.APIBaseURL, entries)
}

// --- MFP API types ---

type mfpResult struct {
	Date  any     `json:"date"`  // "M/D" string or numeric
	Total float64 `json:"total"` // some endpoints return string, so we parse carefully
}

type mfpReportResponse struct {
	Outcome struct {
		Results []mfpResult `json:"results"`
	} `json:"outcome"`
}

type nutritionEntry struct {
	Date     string  `json:"date"`
	Calories float64 `json:"calories"`
	Carbs    float64 `json:"carbs"`
	Fat      float64 `json:"fat"`
	Protein  float64 `json:"protein"`
}

func fetchMFPNutrition(ctx context.Context, cookie string) ([]nutritionEntry, error) {
	endpoints := map[string]string{
		"calories": "https://www.myfitnesspal.com/api/services/reports/results/nutrition/Calories/90?report_name=Calories",
		"carbs":    "https://www.myfitnesspal.com/api/services/reports/results/nutrition/carbs/90?report_name=carbs",
		"fat":      "https://www.myfitnesspal.com/api/services/reports/results/nutrition/fat/90?report_name=fat",
		"protein":  "https://www.myfitnesspal.com/api/services/reports/results/nutrition/protein/90?report_name=protein",
	}

	fetch := func(url string) ([]mfpResult, error) {
		req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
		req.Header.Set("Accept", "application/json, text/plain, */*")
		req.Header.Set("Cookie", "__Secure-next-auth.session-token="+cookie)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("MFP status %d: %s", resp.StatusCode, string(body))
		}
		var r mfpReportResponse
		if err := json.Unmarshal(body, &r); err != nil {
			return nil, err
		}
		return r.Outcome.Results, nil
	}

	type result struct {
		data []mfpResult
		err  error
	}
	results := make([]result, 4)
	keys := []string{"calories", "carbs", "fat", "protein"}
	var wg sync.WaitGroup
	for i, key := range keys {
		wg.Add(1)
		go func(i int, key string) {
			defer wg.Done()
			data, err := fetch(endpoints[key])
			results[i] = result{data, err}
		}(i, key)
	}
	wg.Wait()
	for i, key := range keys {
		if results[i].err != nil {
			return nil, fmt.Errorf("%s: %w", key, results[i].err)
		}
	}
	cals := results[0].data
	carbs := results[1].data
	fats := results[2].data
	proteins := results[3].data

	now := time.Now()
	byDate := map[string]*nutritionEntry{}

	addSeries := func(results []mfpResult, key string) {
		for _, item := range results {
			dateStr := resolveMFPDate(fmt.Sprintf("%v", item.Date), now)
			if dateStr == "" {
				continue
			}
			if byDate[dateStr] == nil {
				byDate[dateStr] = &nutritionEntry{Date: dateStr}
			}
			val := item.Total
			switch key {
			case "calories":
				byDate[dateStr].Calories = val
			case "carbs":
				byDate[dateStr].Carbs = val
			case "fat":
				byDate[dateStr].Fat = val
			case "protein":
				byDate[dateStr].Protein = val
			}
		}
	}

	addSeries(cals, "calories")
	addSeries(carbs, "carbs")
	addSeries(fats, "fat")
	addSeries(proteins, "protein")

	entries := make([]nutritionEntry, 0, len(byDate))
	for _, e := range byDate {
		entries = append(entries, *e)
	}
	return entries, nil
}

// resolveMFPDate converts "M/D" (no year) to "YYYY-MM-DD".
// Dates that appear to be in the future are assigned the previous year.
func resolveMFPDate(raw string, now time.Time) string {
	raw = strings.TrimSpace(raw)
	parts := strings.Split(raw, "/")
	if len(parts) != 2 {
		return ""
	}
	month, err1 := strconv.Atoi(parts[0])
	day, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return ""
	}
	year := now.Year()
	if time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC).After(now) {
		year--
	}
	return fmt.Sprintf("%04d-%02d-%02d", year, month, day)
}

func postNutrition(ctx context.Context, apiBase string, entries []nutritionEntry) error {
	body, _ := json.Marshal(entries)
	req, _ := http.NewRequestWithContext(ctx, "POST", apiBase+"/internal/nutrition", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API nutrition POST returned %d", resp.StatusCode)
	}
	return nil
}
