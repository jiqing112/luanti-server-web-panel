package main

import (
	"encoding/json"
	"io"
	"net/http"
)

func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }

func decodeJSON(resp *http.Response, v any) error {
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(v)
}

var _ = io.Discard
