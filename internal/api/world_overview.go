package api

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/cloudwego/hertz/pkg/app"
	"gorm.io/gorm"
	"jarvis/internal/contextpack"
	"jarvis/internal/worldview"
	"strconv"
)

func WorldOverview(db *gorm.DB) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		f := worldview.Filter{Section: c.Query("section"), Query: c.Query("query")}
		for name, dst := range map[string]*int{"offset": &f.Offset, "limit": &f.Limit} {
			if s := c.Query(name); s != "" {
				n, err := strconv.Atoi(s)
				if err != nil {
					writeAPIError(c, 400, 40080, err)
					return
				}
				*dst = n
			}
		}
		if s := c.Query("task_id"); s != "" {
			n, err := strconv.ParseUint(s, 10, 64)
			if err != nil || n == 0 {
				writeAPIError(c, 400, 40080, fmt.Errorf("task_id must be positive"))
				return
			}
			f.TaskID = n
		}
		v, err := worldview.Read(ctx, db, f)
		if err != nil {
			writeAPIError(c, 400, 40080, err)
			return
		}
		c.JSON(200, map[string]any{"code": 0, "data": v})
	}
}
func contextRange(c *app.RequestContext, raw json.RawMessage) (json.RawMessage, error) {
	if c.Query("offset") == "" && c.Query("length") == "" {
		return raw, nil
	}
	offset := 0
	length := 10000
	var err error
	if s := c.Query("offset"); s != "" {
		offset, err = strconv.Atoi(s)
		if err != nil {
			return nil, err
		}
	}
	if s := c.Query("length"); s != "" {
		length, err = strconv.Atoi(s)
		if err != nil {
			return nil, err
		}
	}
	return contextpack.Range(raw, offset, length)
}
