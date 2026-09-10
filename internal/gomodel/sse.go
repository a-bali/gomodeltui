package gomodel

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

type Event struct {
	ID    string
	Event string
	Data  json.RawMessage
}

func ReadEvent(reader *bufio.Reader) (Event, error) {
	var event Event
	var data bytes.Buffer
	for {
		line, err := reader.ReadBytes('\n')
		line = bytes.TrimSuffix(bytes.TrimSuffix(line, []byte("\n")), []byte("\r"))
		if len(line) == 0 {
			if event.ID == "" && event.Event == "" && data.Len() == 0 {
				if err != nil {
					return Event{}, err
				}
				continue
			}
			event.Data = json.RawMessage(bytes.TrimSpace(data.Bytes()))
			return event, nil
		}
		if bytes.HasPrefix(line, []byte(":")) {
			if err != nil {
				return Event{}, err
			}
			continue
		}
		field, value, ok := bytes.Cut(line, []byte(":"))
		if !ok {
			field, value = line, nil
		}
		value = bytes.TrimPrefix(value, []byte(" "))
		switch string(field) {
		case "id":
			event.ID = string(value)
		case "event":
			event.Event = string(value)
		case "data":
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.Write(value)
		}
		if err != nil {
			if err == io.EOF && data.Len() > 0 {
				continue
			}
			return Event{}, fmt.Errorf("read SSE event: %w", err)
		}
	}
}
