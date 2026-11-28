package mocking

import (
	"encoding/json"
	"testing"
)

// A list written out in an example's response body renders as written. It used to
// render as one item, the first, so an example that listed three products or eight
// samples answered with one.
func TestAListInAnExampleRendersEveryItem(t *testing.T) {
	t.Parallel()
	examples, err := parseSchemaExamplesFromCUE(`
examples: {
  "grid": {
    dispatch: []
    requestSchema: {
      headers: {}
      body: null
    }
    responseSchema: {
      status: "200"
      mediaType: "application/json"
      body: {
        times: [0.0, 0.5, 1.0]
        names: ["a"]
        products: [{sku: "one"}, {sku: "two"}]
      }
    }
  }
}
`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(examples) != 1 {
		t.Fatalf("got %d examples, want 1", len(examples))
	}
	rendered := renderSchemaBody(examples[0].Response.Body, "application/json", map[string]any{})
	var body struct {
		Times    []float64 `json:"times"`
		Names    []string  `json:"names"`
		Products []struct {
			SKU string `json:"sku"`
		} `json:"products"`
	}
	if err := json.Unmarshal([]byte(rendered), &body); err != nil {
		t.Fatalf("rendered body is not JSON: %v\n%s", err, rendered)
	}
	if len(body.Times) != 3 || len(body.Names) != 1 || len(body.Products) != 2 || body.Products[1].SKU != "two" {
		t.Fatalf("lists did not render as written:\n%s", rendered)
	}
}
