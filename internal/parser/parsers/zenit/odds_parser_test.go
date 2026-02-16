package zenit

import (
	"testing"

	"github.com/Vodeneev/vodeneevbet/internal/pkg/models"
)

func TestInferOutcomeType(t *testing.T) {
	tests := []struct {
		oddKey  string
		param   string
		tableID string
		o       string
		t       string
		want    string
	}{
		// Тоталы: O "1" = under, "2" = over (Zenit API convention - reversed)
		{"x|11|2", "2", "Тоталы", "1", "", string(models.OutcomeTypeTotalUnder)},
		{"x|11|2", "2", "Тоталы", "2", "", string(models.OutcomeTypeTotalOver)},
		{"x|11|2.5", "2.5", "ТоталМатча", "", "1", string(models.OutcomeTypeTotalUnder)},
		{"x|11|2.5", "2.5", "ТоталМатча", "", "2", string(models.OutcomeTypeTotalOver)},
		{"x|11|3", "3", "Тоталы", "9", "", string(models.OutcomeTypeTotalUnder)},
		{"x|11|3", "3", "Тоталы", "10", "", string(models.OutcomeTypeTotalOver)},
		// Форы: always exact_count
		{"x|9|-1", "-1", "Форы", "1", "", string(models.OutcomeTypeExactCount)},
		{"x|9|-1.5", "-1.5", "Форы", "2", "", string(models.OutcomeTypeExactCount)},
		// Statistical (corners etc.): 1=under, 2=over (Zenit API convention - reversed)
		{"x|12|10", "10", "Угловые", "1", "", string(models.OutcomeTypeTotalUnder)},
		{"x|12|10", "10", "Угловые", "2", "", string(models.OutcomeTypeTotalOver)},
		// No O/T or unknown code -> exact_count
		{"x|11|2", "2", "Тоталы", "", "", string(models.OutcomeTypeExactCount)},
		{"x|11|2", "2", "Тоталы", "3", "", string(models.OutcomeTypeExactCount)},
		// Invalid oddKey / no param
		{"x", "", "Тоталы", "1", "", string(models.OutcomeTypeExactCount)},
		{"x|11", "11", "Тоталы", "1", "", string(models.OutcomeTypeTotalUnder)},
	}
	for _, tt := range tests {
		got := InferOutcomeType(tt.oddKey, tt.param, tt.tableID, tt.o, tt.t)
		if got != tt.want {
			t.Errorf("InferOutcomeType(%q, %q, %q, %q, %q) = %q, want %q",
				tt.oddKey, tt.param, tt.tableID, tt.o, tt.t, got, tt.want)
		}
	}
}

func TestParseParamFromOddKey(t *testing.T) {
	tests := []struct {
		oddKey string
		want   string
	}{
		{"22790570|11|2", "2"},
		{"22790570|9|-3.5", "-3.5"},
		{"x|y", "y"},
		{"x", ""},
	}
	for _, tt := range tests {
		got := ParseParamFromOddKey(tt.oddKey)
		if got != tt.want {
			t.Errorf("ParseParamFromOddKey(%q) = %q, want %q", tt.oddKey, got, tt.want)
		}
	}
}
