package catalog

import (
	"errors"
	"strings"
)

// The legacy projection remains available to early API/export clients. Original
// intake JSON is never rewritten, so old and new snapshots remain readable.
func splitDescription(value string) (string, string) {
	parts := strings.SplitN(value, "\n", 2)
	short := strings.TrimRight(parts[0], "\r")
	if len(parts) == 1 {
		return short, ""
	}
	return short, parts[1]
}

func combinedDescription(short, details string) string {
	if details == "" {
		return short
	}
	return short + "\n" + details
}

func (c *Asset) setLegacyDescription(value string) {
	c.Description = value
	c.ShortDescription, c.Details = splitDescription(value)
}

func (c *Asset) projectDescription() {
	c.Description = combinedDescription(c.ShortDescription, c.Details)
}

// Both HTML forms and multipart API intake accept the legacy field, but never
// mix it with independently editable fields in one request.
func descriptionForm(values map[string][]string, c *Asset) error {
	_, legacy := values["description"]
	_, short := values["short_description"]
	_, details := values["details"]
	if legacy && (short || details) {
		return errors.New("use description or short_description/details, not both")
	}
	for _, key := range []string{"description", "short_description", "details"} {
		if items, ok := values[key]; ok && len(items) != 1 {
			return errors.New("submit one value for " + key)
		}
	}
	if legacy {
		value := strings.TrimSpace(values["description"][0])
		if len(value) > 20000 {
			return errors.New("legacy description must be 20,000 UTF-8 bytes or fewer")
		}
		c.setLegacyDescription(value)
	} else {
		if short {
			c.ShortDescription = strings.TrimSpace(values["short_description"][0])
		}
		if details {
			c.Details = values["details"][0]
		}
		c.projectDescription()
	}
	return nil
}

func normalizedFormText(value string) string {
	return strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(value)
}
