package a2aserver_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestAgentCardSkillCarriesOutputSchemaForTypedWorkflow(t *testing.T) {
	ts := newTestServer(t, map[string]string{"g.mh": greet + `
type In = { name: string }
type Out = { message: string }
workflow TypedGreet(req: In): Out {
    step S { log(req.name) }
    output: { message: "hi " + req.name }
}
`})
	resp, err := http.Get(ts.URL + "/.well-known/agent-card.json")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var card map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&card); err != nil {
		t.Fatal(err)
	}
	meta := map[string]map[string]any{}
	for _, raw := range card["skills"].([]any) {
		sk := raw.(map[string]any)
		meta[sk["id"].(string)] = sk["metadata"].(map[string]any)
	}
	out, ok := meta["TypedGreet"]["outputSchema"].(map[string]any)
	if !ok || out["title"] != "Out" {
		t.Fatalf("typed skill metadata.outputSchema = %v", meta["TypedGreet"])
	}
	if _, has := meta["Greet"]["outputSchema"]; has {
		t.Fatalf("untyped skill must carry no outputSchema: %v", meta["Greet"])
	}
}
