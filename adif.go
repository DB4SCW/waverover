package main

import (
	"bufio"
	"bytes"
	"io"
	"strings"
	"unicode"
)

type QSO struct {
	Fields map[string]string
	Raw    string
}

func ParseADIF(r io.Reader) ([]QSO, error) {
	br := bufio.NewReaderSize(r, 256*1024)
	headerSkipped := false
	var qsos []QSO
	var buf bytes.Buffer
	var readErr error

	extractRecords := func() {
		for {
			data := buf.Bytes()
			idx := indexTagBytes(data, "eor")
			if idx < 0 {
				return
			}
			rec := strings.TrimSpace(string(data[:idx]))
			buf.Reset()
			if idx+5 < len(data) {
				buf.Write(data[idx+5:])
			}
			if rec == "" {
				continue
			}
			fields := parseFields(rec)
			if len(fields) > 0 {
				qsos = append(qsos, QSO{Fields: fields, Raw: rec})
			}
		}
	}

	for {
		line, err := br.ReadSlice('\n')
		if len(line) > 0 {
			buf.Write(line)
		}

		if !headerSkipped {
			if idx := indexTagBytes(buf.Bytes(), "eoh"); idx >= 0 {
				remaining := make([]byte, len(buf.Bytes())-idx-5)
				copy(remaining, buf.Bytes()[idx+5:])
				buf.Reset()
				buf.Write(remaining)
				headerSkipped = true
				continue
			}
			if indexTagBytes(buf.Bytes(), "eor") >= 0 {
				headerSkipped = true
			} else {
				if !bytes.Contains(bytes.ToLower(buf.Bytes()), []byte("<")) {
					buf.Reset()
				}
				if err != nil {
					if err != io.EOF {
						readErr = err
					}
					break
				}
				continue
			}
		}

		extractRecords()

		if err != nil {
			if err != io.EOF {
				readErr = err
			}
			break
		}
	}

	extractRecords()
	return qsos, readErr
}

func parseFields(record string) map[string]string {
	fields := make(map[string]string)
	i := 0
	for i < len(record) {
		start := strings.IndexByte(record[i:], '<')
		if start < 0 {
			break
		}
		i += start + 1
		if i >= len(record) {
			break
		}
		end := strings.IndexByte(record[i:], '>')
		if end < 0 {
			break
		}
		tag := record[i : i+end]
		i = i + end + 1
		name, length := parseTag(tag)
		if name == "" || length <= 0 {
			continue
		}
		if i+length > len(record) {
			continue
		}
		fields[strings.ToUpper(name)] = record[i : i+length]
		i += length
	}
	return fields
}

func parseTag(tag string) (string, int) {
	parts := strings.SplitN(tag, ":", 3)
	if len(parts) < 2 {
		return "", 0
	}
	name := strings.TrimSpace(parts[0])
	length := 0
	for _, ch := range parts[1] {
		if unicode.IsDigit(ch) {
			length = length*10 + int(ch-'0')
		} else {
			break
		}
	}
	return name, length
}

func indexTagBytes(data []byte, tagname string) int {
	target := []byte("<" + strings.ToUpper(tagname) + ">")
	return bytes.Index(bytes.ToUpper(data), target)
}

func (q *QSO) GetField(names ...string) string {
	for _, n := range names {
		if v, ok := q.Fields[strings.ToUpper(n)]; ok {
			return v
		}
	}
	return ""
}
