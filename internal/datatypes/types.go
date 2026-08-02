// Package datatypes contains the two database value types Jarvis actually uses.
package datatypes

import (
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// JSON stores an untyped JSON value without importing GORM's multi-database
// datatype package.
type JSON json.RawMessage

func (value JSON) Value() (driver.Value, error) {
	if len(value) == 0 {
		return nil, nil
	}
	if !json.Valid(value) {
		return nil, fmt.Errorf("invalid JSON value")
	}
	return string(value), nil
}

func (value *JSON) Scan(source any) error {
	if source == nil {
		*value = JSON("null")
		return nil
	}
	var raw []byte
	switch typed := source.(type) {
	case []byte:
		raw = append(raw, typed...)
	case string:
		raw = []byte(typed)
	default:
		return fmt.Errorf("scan JSON from %T", source)
	}
	if !json.Valid(raw) {
		return fmt.Errorf("scan invalid JSON")
	}
	*value = JSON(raw)
	return nil
}

func (value JSON) MarshalJSON() ([]byte, error) {
	return json.RawMessage(value).MarshalJSON()
}

func (value *JSON) UnmarshalJSON(raw []byte) error {
	if !json.Valid(raw) {
		return fmt.Errorf("unmarshal invalid JSON")
	}
	*value = append((*value)[:0], raw...)
	return nil
}

func (value JSON) String() string { return string(value) }

func (JSON) GormDataType() string { return "json" }

func (JSON) GormDBDataType(*gorm.DB, *schema.Field) string { return "JSON" }

// Date stores a calendar date at midnight in its original location.
type Date time.Time

func (date *Date) Scan(source any) error {
	var value sql.NullTime
	if err := value.Scan(source); err != nil {
		return err
	}
	*date = Date(value.Time)
	return nil
}

func (date Date) Value() (driver.Value, error) {
	value := time.Time(date)
	year, month, day := value.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, value.Location()), nil
}

func (Date) GormDataType() string { return "date" }

func (Date) GormDBDataType(*gorm.DB, *schema.Field) string { return "DATE" }

func (date Date) MarshalJSON() ([]byte, error) {
	return time.Time(date).MarshalJSON()
}

func (date *Date) UnmarshalJSON(raw []byte) error {
	return (*time.Time)(date).UnmarshalJSON(raw)
}
