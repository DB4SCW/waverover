package main

import (
	"fmt"
	"regexp"
	"strings"
)

var placeholderRe = regexp.MustCompile(`\{[A-Za-z_]+\}`)

// placeholderField extracts the uppercased field name from a "{FIELD}" match.
func placeholderField(m string) string {
	return strings.ToUpper(m[1 : len(m)-1])
}

type StationLocation struct {
	Key   string
	QSOs  []QSO
	Match map[string]string
}

// FieldAliases maps ADIF field names that have common variant spellings.
// Fields not listed here are looked up by their exact name.
var FieldAliases = map[string][]string{
	"STATION_CALLSIGN": {"STATION_CALLSIGN", "MY_STATION_CALLSIGN", "OPERATOR"},
	"MY_GRIDSQUARE":    {"MY_GRIDSQUARE", "MY_GRID"},
}

func GroupByLocation(qsos []QSO, matchFields []string, gridPrecision int) []StationLocation {
	locMap := make(map[string]*StationLocation)

	for _, qso := range qsos {
		values := extractMatchValues(qso, matchFields, gridPrecision)
		key := locationKey(values, matchFields)

		loc, ok := locMap[key]
		if !ok {
			loc = &StationLocation{Key: key, Match: values}
			locMap[key] = loc
		}
		loc.QSOs = append(loc.QSOs, qso)
	}

	result := make([]StationLocation, 0, len(locMap))
	for _, loc := range locMap {
		result = append(result, *loc)
	}
	return result
}

func extractMatchValues(qso QSO, matchFields []string, gridPrecision int) map[string]string {
	values := make(map[string]string)
	for _, field := range matchFields {
		upper := strings.ToUpper(field)
		var val string
		if aliases, ok := FieldAliases[upper]; ok {
			val = qso.GetField(aliases...)
		} else {
			val = qso.GetField(upper)
		}

		if isGridField(upper) && val != "" {
			val = normalizeGrid(val, gridPrecision)
		}
		values[upper] = val
	}
	return values
}

func locationKey(values map[string]string, matchFields []string) string {
	parts := make([]string, 0, len(matchFields))
	for _, f := range matchFields {
		parts = append(parts, values[strings.ToUpper(f)])
	}
	return strings.Join(parts, "|")
}

func isGridField(name string) bool {
	return strings.Contains(name, "GRID") || strings.Contains(name, "GRIDSQUARE") || strings.Contains(name, "VUCC_GRIDS")
}

func normalizeGrid(grid string, precision int) string {
	grid = strings.ToUpper(strings.TrimSpace(grid))
	if len(grid) > precision {
		grid = grid[:precision]
	}
	return grid
}

// DisplayName builds the profile name. With nameFormat, {FIELD} placeholders
// are replaced by the location's match values. Without it, the name is built
// from all non-empty match field values: "FIRST @ REST..." .
func (sl *StationLocation) DisplayName(matchFields []string, nameFormat string) string {
	if nameFormat != "" {
		name := placeholderRe.ReplaceAllStringFunc(nameFormat, func(m string) string {
			return sl.Match[placeholderField(m)]
		})
		if name = strings.TrimSpace(name); name != "" {
			return name
		}
	}

	var parts []string
	for _, f := range matchFields {
		if v := sl.Match[strings.ToUpper(f)]; v != "" {
			parts = append(parts, v)
		}
	}
	switch len(parts) {
	case 0:
		return sl.Key
	case 1:
		return parts[0]
	default:
		return fmt.Sprintf("%s @ %s", parts[0], strings.Join(parts[1:], " "))
	}
}
