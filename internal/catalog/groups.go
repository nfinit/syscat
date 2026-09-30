package catalog

import (
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

const maxGroupLength = 100

type sectionPhoto struct {
	Photo
	Position int
}
type photoSection struct {
	Name     string
	Title    string
	Overview bool
	Position int
	Photos   []sectionPhoto
}

// Group order and photo order are represented by the flat photo array. Overview
// is always first and independent of named groups; ungrouped detail photos last.
func (c Asset) PhotoSections() []photoSection {
	if len(c.Photos) == 0 {
		return nil
	}
	sections := []photoSection{{Title: "Overview", Overview: true, Photos: []sectionPhoto{{c.Photos[0], 1}}}}
	indexes := map[string]int{}
	var ungrouped []sectionPhoto
	for i, p := range c.Photos[1:] {
		item := sectionPhoto{p, i + 2}
		if p.Group == "" {
			ungrouped = append(ungrouped, item)
			continue
		}
		index, ok := indexes[p.Group]
		if !ok {
			index = len(sections)
			indexes[p.Group] = index
			sections = append(sections, photoSection{Name: p.Group, Title: p.Group, Position: index})
		}
		sections[index].Photos = append(sections[index].Photos, item)
	}
	if len(ungrouped) > 0 {
		sections = append(sections, photoSection{Title: "Photos", Photos: ungrouped})
	}
	return sections
}

func validateGroup(group string) error {
	if utf8.RuneCountInString(group) > maxGroupLength {
		return errors.New("photo group names must be 100 characters or fewer")
	}
	if strings.ContainsAny(group, "\r\n\x00") {
		return errors.New("photo group names must be a single line")
	}
	return nil
}

func submittedGroups(values url.Values, photos []Photo) error {
	for i := range photos {
		if v, ok := values["group_"+photos[i].Path]; ok {
			if len(v) != 1 {
				return errors.New("submit one group per photo")
			}
			photos[i].Group = strings.TrimSpace(v[0])
		}
		if err := validateGroup(photos[i].Group); err != nil {
			return err
		}
	}
	return nil
}

func submittedGroupOrder(values url.Values) ([]string, error) {
	type ranked struct {
		name     string
		position int
	}
	var ranks []ranked
	for key, v := range values {
		if !strings.HasPrefix(key, "group_position_") {
			continue
		}
		name := strings.TrimPrefix(key, "group_position_")
		if name == "" || len(v) != 1 {
			return nil, errors.New("assign one position to each named photo group")
		}
		if err := validateGroup(name); err != nil {
			return nil, err
		}
		n, err := strconv.Atoi(v[0])
		if err != nil || n < 1 {
			return nil, errors.New("photo group positions must be positive integers")
		}
		ranks = append(ranks, ranked{name, n})
	}
	sort.Slice(ranks, func(i, j int) bool { return ranks[i].position < ranks[j].position })
	names := make([]string, len(ranks))
	for i, r := range ranks {
		if r.position != i+1 {
			return nil, fmt.Errorf("photo group positions must use each number from 1 to %d once", len(ranks))
		}
		names[i] = r.name
	}
	return names, nil
}

func selectedOverview(values url.Values, existing, uploads []Photo) (string, error) {
	choices := values["overview_choice"]
	if len(choices) == 0 {
		if len(existing) > 0 {
			return existing[0].Path, nil
		}
		if len(uploads) > 0 {
			return uploads[0].Path, nil
		}
		return "", nil
	}
	if len(choices) != 1 {
		return "", errors.New("choose one overview photo")
	}
	choice := choices[0]
	if strings.HasPrefix(choice, "upload:") {
		raw := strings.TrimPrefix(choice, "upload:")
		n, err := strconv.Atoi(raw)
		if err != nil || strconv.Itoa(n) != raw || n < 0 || n >= len(uploads) {
			return "", errors.New("the selected overview upload is no longer attached; reselect the photo")
		}
		return uploads[n].Path, nil
	}
	for _, p := range existing {
		if p.Path == choice {
			return choice, nil
		}
	}
	return "", errors.New("the selected overview photo is not attached to this entry")
}

func groupedPhotos(photos []Photo, overview, formerOverview string, groupOrder []string) []Photo {
	if len(photos) == 0 {
		return photos
	}
	var first Photo
	groups := map[string][]Photo{}
	var names []string
	for _, p := range photos {
		if p.Path == overview {
			p.Group = ""
			first = p
			continue
		}
		if p.Path == formerOverview {
			p.Group = ""
		}
		if _, ok := groups[p.Group]; !ok {
			names = append(names, p.Group)
		}
		groups[p.Group] = append(groups[p.Group], p)
	}
	result := []Photo{first}
	used := map[string]bool{"": true}
	for _, name := range append(groupOrder, names...) {
		if used[name] {
			continue
		}
		used[name] = true
		result = append(result, groups[name]...)
	}
	return append(result, groups[""]...)
}
