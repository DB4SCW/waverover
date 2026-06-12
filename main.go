package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

func main() {
	url := flag.String("url", "", "Wavelog base URL or set WAVELOG_URL")
	key := flag.String("key", "", "Wavelog API key (read+write) or set WAVELOG_API_KEY")
	dryRun := flag.Bool("dry-run", false, "Show what would happen without making changes")
	matchFields := flag.String("match-fields", "STATION_CALLSIGN,MY_GRIDSQUARE", "Comma-separated ADIF fields for station location matching")
	nameFormat := flag.String("name-format", "", "Profile name template with {FIELD} placeholders from match fields, e.g. \"{STATION_CALLSIGN} @ {MY_SOTA_REF}\" (empty: auto from match fields)")
	gridPrecision := flag.Int("grid-precision", 6, "Grid locator precision (4 or 6)")
	flag.Parse()

	waveURL := coalesce(*url, os.Getenv("WAVELOG_URL"))
	waveKey := coalesce(*key, os.Getenv("WAVELOG_API_KEY"))
	files := expandFiles(flag.Args())

	if waveURL == "" || waveKey == "" || len(files) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: waveRover -url <url> -key <key> [-dry-run] <file.adi> [file2.adi ...]  (wildcards supported, e.g. *.adi)")
		fmt.Fprintln(os.Stderr, "\nFlags:")
		flag.PrintDefaults()
		fmt.Fprintln(os.Stderr, "\nEnvironment variables:")
		fmt.Fprintln(os.Stderr, "  WAVELOG_URL      Wavelog base URL")
		fmt.Fprintln(os.Stderr, "  WAVELOG_API_KEY  Wavelog API key")
		os.Exit(1)
	}

	fields := parseMatchFields(*matchFields)
	warnUnknownPlaceholders(*nameFormat, fields)
	fmt.Printf("Match fields: %v\nGrid precision: %d\nDry run: %v\n\n", fields, *gridPrecision, *dryRun)

	var allQSOs []QSO
	for _, file := range files {
		fmt.Printf("Parsing %s...\n", file)
		qsos, err := parseFile(file)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing %s: %v\n", file, err)
			os.Exit(1)
		}
		fmt.Printf("  Found %d QSOs\n", len(qsos))
		allQSOs = append(allQSOs, qsos...)
	}
	fmt.Printf("\nTotal QSOs: %d\n\n", len(allQSOs))

	if len(allQSOs) == 0 {
		fmt.Println("No QSOs found. Exiting.")
		return
	}

	locations := GroupByLocation(allQSOs, fields, *gridPrecision)
	fmt.Printf("Unique station locations: %d\n\n", len(locations))
	for i, loc := range locations {
		fmt.Printf("  [%d] %s (%d QSOs)\n", i+1, loc.DisplayName(fields, *nameFormat), len(loc.QSOs))
	}
	fmt.Println()

	if *dryRun {
		fmt.Println("Dry run — no changes made.")
		for _, loc := range locations {
			fmt.Printf("  Would import: %s (%d QSOs)\n", loc.DisplayName(fields, *nameFormat), len(loc.QSOs))
		}
		return
	}

	client := NewWavelogClient(waveURL, waveKey)
	version, err := client.CheckVersion()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to connect to Wavelog: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Connected to Wavelog v%s\n\n", version)

	profiles, returnedFields, err := client.GetStationProfiles()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to fetch station profiles: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Existing station profiles: %d\n", len(profiles))
	for _, p := range profiles {
		fmt.Printf("  #%s: %s (%s @ %s)\n", p.ID, p.Raw["station_profile_name"], p.Raw["station_callsign"], p.Raw["station_gridsquare"])
	}
	WarnUnsupportedFields(fields, returnedFields)
	fmt.Println()

	var allResults []ImportResult
	for i, loc := range locations {
		label := loc.DisplayName(fields, *nameFormat)
		fmt.Printf("[%d/%d] Processing %s (%d QSOs)\n", i+1, len(locations), label, len(loc.QSOs))

		profileID, err := MatchProfile(loc, profiles, fields, returnedFields)
		if err != nil {
			fmt.Println("  No matching profile found, creating new station location...")
			profileID, err = client.CreateStationProfile(loc, fields, *nameFormat)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  ERROR creating profile: %v\n", err)
				for j := range loc.QSOs {
					allResults = append(allResults, ImportResult{QSO: &loc.QSOs[j], Status: "error", Err: err})
				}
				continue
			}
			profiles, returnedFields, _ = client.GetStationProfiles()
		} else {
			fmt.Printf("  Matched existing profile #%s\n", profileID)
		}

		allResults = append(allResults, client.ImportQSOsChunked(profileID, loc.QSOs, *gridPrecision, label)...)
	}

	printSummary(allResults)
}

func printSummary(results []ImportResult) {
	var imported, dupes, errors int
	for _, r := range results {
		switch r.Status {
		case "imported":
			imported++
		case "dupe":
			dupes++
		case "error":
			errors++
		}
	}

	fmt.Println("=== Summary ===")
	fmt.Printf("Imported: %d\n", imported)
	if dupes > 0 {
		fmt.Printf("Duplicates skipped: %d\n", dupes)
	}
	if errors > 0 {
		fmt.Printf("Errors: %d\n", errors)
	}

	if dupes > 0 {
		fmt.Println("\nDuplicates:")
		for _, r := range results {
			if r.Status == "dupe" {
				fmt.Printf("  %s\n", qsoLabel(r.QSO))
			}
		}
	}
	if errors > 0 {
		fmt.Println("\nErrors:")
		for _, r := range results {
			if r.Status == "error" {
				fmt.Printf("  %s — %v\n", qsoLabel(r.QSO), r.Err)
			}
		}
	}
}

func qsoLabel(q *QSO) string {
	parts := []string{q.GetField("CALL"), q.GetField("BAND"), q.GetField("MODE"), q.GetField("QSO_DATE")}
	if t := q.GetField("TIME_ON"); t != "" {
		parts = append(parts, t)
	}
	return strings.Join(parts, " ")
}

func expandFiles(files []string) []string {
	var expanded []string
	for _, f := range files {
		matches, err := filepath.Glob(f)
		if err != nil || len(matches) == 0 {
			expanded = append(expanded, f)
		} else {
			expanded = append(expanded, matches...)
		}
	}
	return expanded
}

func parseFile(path string) ([]QSO, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening file: %w", err)
	}
	defer f.Close()
	return ParseADIF(f)
}

func warnUnknownPlaceholders(format string, fields []string) {
	for _, m := range placeholderRe.FindAllString(format, -1) {
		if name := placeholderField(m); !slices.Contains(fields, name) {
			fmt.Fprintf(os.Stderr, "Warning: -name-format placeholder {%s} is not in -match-fields, will be empty\n", name)
		}
	}
}

func parseMatchFields(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(strings.ToUpper(p)); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func coalesce(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
