package components

import "github.com/a-h/templ"

// Invalid marks the control id as failing with msg, keeping any aria-describedby hint it already has.
func Invalid(attrs templ.Attributes, id, msg string) templ.Attributes {
	if msg == "" {
		return attrs
	}
	out := templ.Attributes{}
	for k, v := range attrs {
		out[k] = v
	}
	out["aria-invalid"] = "true"
	describedBy := id + "-error"
	if hint, ok := attrs["aria-describedby"].(string); ok && hint != "" {
		describedBy = hint + " " + describedBy
	}
	out["aria-describedby"] = describedBy
	return out
}
