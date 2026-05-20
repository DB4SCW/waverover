package main

import (
	"fmt"
	"strings"
)

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

func (sl *StationLocation) DisplayName(matchFields []string) string {
	call := sl.Match["STATION_CALLSIGN"]
	grid := sl.Match["MY_GRIDSQUARE"]

	if call != "" && grid != "" {
		return fmt.Sprintf("%s @ %s", call, grid)
	}
	if call != "" {
		return call
	}
	return sl.Key
}
