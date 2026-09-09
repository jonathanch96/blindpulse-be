package validator

import (
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

type validationPayload struct {
	Currency   string          `json:"currency" validate:"currency"`
	Amount     decimal.Decimal `json:"amount" validate:"decimalgt=0"`
	Adjustment decimal.Decimal `json:"adjustment" validate:"decimalgte=0"`
	StartDate  time.Time       `json:"start_date"`
	EndDate    time.Time       `json:"end_date" validate:"dateafter=StartDate"`
	Name       string          `json:"name" validate:"notblank"`
}

func validPayload() validationPayload {
	start := time.Date(2026, time.August, 5, 0, 0, 0, 0, time.UTC)
	return validationPayload{
		Currency:   "USD",
		Amount:     decimal.RequireFromString("1.25"),
		Adjustment: decimal.Zero,
		StartDate:  start,
		EndDate:    start.Add(24 * time.Hour),
		Name:       "Dinner",
	}
}

func TestCustomRules(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		tag  string
		edit func(*validationPayload)
	}{
		{"currency", "currency", func(value *validationPayload) { value.Currency = "ZZZ" }},
		{"decimalgt", "decimalgt", func(value *validationPayload) { value.Amount = decimal.Zero }},
		{"decimalgte", "decimalgte", func(value *validationPayload) { value.Adjustment = decimal.NewFromInt(-1) }},
		{"dateafter", "dateafter", func(value *validationPayload) { value.EndDate = value.StartDate.Add(-time.Hour) }},
		{"notblank", "notblank", func(value *validationPayload) { value.Name = "  \t" }},
	}
	validate := New()
	if err := validate.Struct(validPayload()); err != nil {
		t.Fatalf("valid payload rejected: %v", err)
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := validPayload()
			test.edit(&value)
			err := validate.Struct(value)
			if err == nil {
				t.Fatal("invalid payload accepted")
			}
			fields := Translate(err)
			if len(fields) != 1 || fields[0].Rule != test.tag {
				t.Fatalf("unexpected translated errors: %+v", fields)
			}
		})
	}
}

func TestTranslateUsesJSONFieldNames(t *testing.T) {
	t.Parallel()
	value := validPayload()
	value.Currency = "invalid"
	fields := Translate(New().Struct(value))
	if len(fields) != 1 || fields[0].Field != "currency" {
		t.Fatalf("expected JSON field name, got %+v", fields)
	}
}

func TestTranslateHandlesNonValidationError(t *testing.T) {
	t.Parallel()
	fields := Translate(errors.New("binding failed"))
	if len(fields) != 1 || fields[0].Field != "request" || fields[0].Rule != "invalid" {
		t.Fatalf("unexpected fallback: %+v", fields)
	}
}
