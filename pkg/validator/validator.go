package validator

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/jblabs/blindpulse-be/pkg/response"
	"github.com/shopspring/decimal"
)

var supportedCurrencies = map[string]struct{}{
	"AUD": {}, "CAD": {}, "CHF": {}, "CNY": {}, "EUR": {}, "GBP": {}, "HKD": {},
	"IDR": {}, "INR": {}, "JPY": {}, "KRW": {}, "MYR": {}, "NZD": {}, "PHP": {},
	"SGD": {}, "THB": {}, "USD": {}, "VND": {},
}

type Validator struct{ validate *validator.Validate }

func New() *Validator {
	v := validator.New()
	v.RegisterTagNameFunc(func(field reflect.StructField) string {
		name := strings.SplitN(field.Tag.Get("json"), ",", 2)[0]
		if name == "-" || name == "" {
			return field.Name
		}
		return name
	})
	_ = v.RegisterValidation("currency", currency)
	_ = v.RegisterValidation("decimalgt", decimalGT)
	_ = v.RegisterValidation("decimalgte", decimalGTE)
	_ = v.RegisterValidation("dateafter", dateAfter)
	_ = v.RegisterValidation("notblank", notBlank)
	return &Validator{validate: v}
}

func (v *Validator) Struct(value any) error { return v.validate.Struct(value) }

func (v *Validator) ValidateStruct(value any) error { return v.validate.Struct(value) }

func (v *Validator) Engine() any { return v.validate }

func Translate(err error) []response.FieldError {
	validationErrors, ok := err.(validator.ValidationErrors)
	if !ok {
		return []response.FieldError{{Field: "request", Rule: "invalid", Message: "request is invalid"}}
	}
	fields := make([]response.FieldError, 0, len(validationErrors))
	for _, item := range validationErrors {
		field := item.Namespace()
		if dot := strings.IndexByte(field, '.'); dot >= 0 {
			field = field[dot+1:]
		}
		fields = append(fields, response.FieldError{
			Field:   field,
			Rule:    item.Tag(),
			Message: fmt.Sprintf("%s failed the %s validation", field, item.Tag()),
		})
	}
	return fields
}

func currency(fl validator.FieldLevel) bool {
	_, ok := supportedCurrencies[strings.ToUpper(fl.Field().String())]
	return ok
}

func decimalGT(fl validator.FieldLevel) bool {
	value, ok := decimalValue(fl.Field())
	if !ok {
		return false
	}
	threshold, err := decimal.NewFromString(fl.Param())
	return err == nil && value.GreaterThan(threshold)
}

func decimalGTE(fl validator.FieldLevel) bool {
	value, ok := decimalValue(fl.Field())
	if !ok {
		return false
	}
	threshold, err := decimal.NewFromString(fl.Param())
	return err == nil && value.GreaterThanOrEqual(threshold)
}

func dateAfter(fl validator.FieldLevel) bool {
	start := fl.Parent().FieldByName(fl.Param())
	if !start.IsValid() || !start.CanInterface() || !fl.Field().CanInterface() {
		return false
	}
	end, okEnd := fl.Field().Interface().(time.Time)
	begin, okStart := start.Interface().(time.Time)
	return okEnd && okStart && !end.Before(begin)
}

func notBlank(fl validator.FieldLevel) bool { return strings.TrimSpace(fl.Field().String()) != "" }

func decimalValue(field reflect.Value) (decimal.Decimal, bool) {
	value, ok := field.Interface().(decimal.Decimal)
	return value, ok
}
