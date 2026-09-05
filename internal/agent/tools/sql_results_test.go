package tools

import "testing"

func TestSQLResultsPreserveNonFiniteNumbers(t *testing.T) {
	db := regressionDuckDB(t)
	rows, err := db.Query(`SELECT CAST('Infinity' AS DOUBLE) AS positive, CAST('-Infinity' AS DOUBLE) AS negative, CAST('NaN' AS DOUBLE) AS undefined, 1.5::DOUBLE AS finite`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	_, result, truncated, err := scanSQLRows(rows, 10, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if truncated || len(result) != 1 {
		t.Fatalf("unexpected result: %v, truncated=%v", result, truncated)
	}
	for key, want := range map[string]interface{}{"positive": "+Inf", "negative": "-Inf", "undefined": "NaN", "finite": float64(1.5)} {
		if result[0][key] != want {
			t.Errorf("%s: got %v, want %v", key, result[0][key], want)
		}
	}
}
