package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// stdinReader is shared across prompts so that buffered (non-interactive) input
// is not lost between successive reads.
var stdinReader = bufio.NewReader(os.Stdin)

// CallsignLookup holds the fields we use from Wavelog's /api/private_lookup
// response. The endpoint resolves DXCC info from a callsign's prefix; note it
// does NOT return an ITU zone.
type CallsignLookup struct {
	DXCC   string `json:"dxcc"`     // country name -> station_country
	DXCCID string `json:"dxcc_id"`  // DXCC entity id -> station_dxcc
	CQZ    string `json:"dxcc_cqz"` // CQ zone -> station_cq
}

// lookupCallsign resolves DXCC info for a callsign via Wavelog, caching results
// so callsigns shared across locations are only fetched once.
func (c *WavelogClient) lookupCallsign(callsign string) (*CallsignLookup, error) {
	callsign = strings.ToUpper(strings.TrimSpace(callsign))
	if callsign == "" {
		return nil, fmt.Errorf("empty callsign")
	}
	if v, ok := c.lookupCache[callsign]; ok {
		return v, nil
	}
	var resp CallsignLookup
	if err := c.post("/api/private_lookup", map[string]string{"key": c.APIKey, "callsign": callsign}, &resp); err != nil {
		return nil, err
	}
	c.lookupCache[callsign] = &resp
	return &resp, nil
}

// mandatoryStationFieldsMissing reports whether any field that Wavelog needs to
// create a station profile (DXCC / CQ / Country) is empty in the payload.
func mandatoryStationFieldsMissing(payload map[string]string) bool {
	return payload["station_dxcc"] == "" || payload["station_cq"] == "" || payload["station_country"] == ""
}

// enrichMandatoryFields resolves the originating station callsign and backfills
// the missing DXCC/CQ/Country fields via Wavelog's lookup. It returns an error
// (so the caller can warn+skip the location) when the callsign can't be resolved
// or the lookup yields no DXCC entity.
func (c *WavelogClient) enrichMandatoryFields(loc StationLocation, payload map[string]string) error {
	call := loc.QSOs[0].GetField(FieldAliases["STATION_CALLSIGN"]...)
	if call == "" {
		return fmt.Errorf("cannot resolve mandatory station fields (DXCC/CQ/Country): no station callsign in QSOs")
	}
	l, err := c.lookupCallsign(call)
	if err != nil {
		return fmt.Errorf("cannot resolve mandatory station fields (DXCC/CQ/Country) for %q via lookup: %w", call, err)
	}
	if l.DXCCID == "" {
		return fmt.Errorf("cannot resolve mandatory station fields (DXCC/CQ/Country) for %q via lookup: no DXCC entity returned", call)
	}
	if filled := applyLookupToPayload(payload, l); len(filled) > 0 {
		fmt.Printf("  Looked up %s → DXCC %s (%s), CQ %s\n", strings.ToUpper(call), l.DXCCID, l.DXCC, l.CQZ)
	}
	return nil
}

// applyLookupToPayload backfills the mandatory station fields from a lookup
// result, filling only keys that are currently empty (never overwriting values
// already present in the ADIF). It returns the keys it filled. If the lookup
// carries no DXCC entity it is treated as empty and nothing is filled.
func applyLookupToPayload(payload map[string]string, l *CallsignLookup) []string {
	if l == nil || l.DXCCID == "" {
		return nil
	}
	var filled []string
	fill := func(key, val string) {
		if val != "" && payload[key] == "" {
			payload[key] = val
			filled = append(filled, key)
		}
	}
	fill("station_dxcc", l.DXCCID)
	fill("station_cq", l.CQZ)
	fill("station_country", l.DXCC)
	return filled
}

// normalizeItuZone returns the canonical ITU zone string ("1".."90") for a
// numeric input, or "" if it is out of the valid 1-90 range or non-numeric.
func normalizeItuZone(s string) string {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 1 || n > 90 {
		return ""
	}
	return strconv.Itoa(n)
}

// resolveItuZone supplies the ITU zone when the ADIF omits MY_ITU_ZONE.
// Wavelog accepts an empty station_itu on profile creation, but then fails every
// QSO import for that profile — so the zone must be set. If ituDefault is a
// valid zone (e.g. supplied via -itu-zone) it is used directly; otherwise the
// user is prompted once per location, scoped to the given profile label.
func resolveItuZone(label, ituDefault string) (string, error) {
	if ituDefault != "" {
		if z := normalizeItuZone(ituDefault); z != "" {
			return z, nil
		}
		return "", fmt.Errorf("invalid ITU zone %q (must be 1-90)", ituDefault)
	}
	fmt.Printf("  Location %q has no MY_ITU_ZONE, but Wavelog needs one to import QSOs.\n  Enter ITU zone (1-90): ", label)
	line, err := stdinReader.ReadString('\n')
	if err != nil && line == "" {
		return "", fmt.Errorf("reading ITU zone: %w", err)
	}
	z := normalizeItuZone(line)
	if z == "" {
		return "", fmt.Errorf("no valid ITU zone provided for %q (must be 1-90)", label)
	}
	return z, nil
}
