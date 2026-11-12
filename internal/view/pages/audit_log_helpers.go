package pages

import (
	"encoding/json"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// auditDetailMax keeps one long value (a request path, a reason) from stretching a row.
const auditDetailMax = 120

type auditDetail struct{ Key, Value string }

// auditDetails lists an entry's metadata as key/value pairs in key order.
func auditDetails(meta map[string]any) []auditDetail {
	keys := make([]string, 0, len(meta))
	for k, v := range meta {
		if v != nil {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	out := make([]auditDetail, 0, len(keys))
	for _, k := range keys {
		out = append(out, auditDetail{Key: k, Value: truncateDetail(auditDetailValue(meta[k]))})
	}
	return out
}

func auditDetailValue(v any) string {
	switch v := v.(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	case []any:
		parts := make([]string, len(v))
		for i, e := range v {
			parts[i] = auditDetailValue(e)
		}
		return strings.Join(parts, ", ")
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

func truncateDetail(s string) string {
	r := []rune(s)
	if len(r) <= auditDetailMax {
		return s
	}
	return string(r[:auditDetailMax-1]) + "…"
}

func auditPageURL(f model.AuditFilter, page int) string {
	q := url.Values{"page": {strconv.Itoa(page)}, "action": {f.Action}, "target_type": {f.TargetType}}
	if f.Sort != "" {
		q.Set("sort", f.Sort)
		q.Set("dir", sortDir(f.Desc))
	}
	return "/admin/audit-log?" + q.Encode()
}

// auditSortURL is the header link for key: page 1 of the same filter, sorted by key.
func auditSortURL(f model.AuditFilter, key string) string {
	cur, desc := auditSortState(f)
	f.Desc = model.AuditSort.Toggle(cur, desc, key)
	f.Sort = key
	return auditPageURL(f, 1)
}

// auditSortState is the sorted column and direction, which a filter built outside the handler leaves empty.
func auditSortState(f model.AuditFilter) (string, bool) {
	if f.Sort == "" {
		return model.AuditSort[0].Key, model.AuditSort[0].DescFirst
	}
	return f.Sort, f.Desc
}

func auditAriaSort(f model.AuditFilter, key string) string {
	cur, desc := auditSortState(f)
	return ariaSort(cur, key, desc)
}
